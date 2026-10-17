<!-- template_id: design; template_version: 1.1.1 -->
# `suga` 上手、配置初始化与模型列表设计

> 状态：Draft 0.1 / 待用户审批

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-17 | suga（gofer job） | 初稿：覆盖首次运行初始化、`suga init`、`suga config`、`suga models`、配置安全写回、模型切换范围、兼容与验证。 |

### 思考模式与范围冻结

- thinking_mode：RIGOROUS。
- core_objective：新用户第一次运行 `suga` 就得到一个安全、可编辑、可解释的配置入口；已有用户可以用 `config get/set/unset/list/path` 维护配置，并用 `models` 在不联网的情况下看清可用模型与当前选择。
- allowed_scope：`cmd/suga`、`internal/cli`、`internal/config`、`internal/wiring`、`internal/chat` 的命令注册或配置复用；`config/` 模板；`skills/suga-guide`、README 中英；相关单测与 Linux `scripts/ptysmoke` 场景。
- non_goals：不改变 provider 协议、不探测远端模型、不创建 `.env`、不把密钥写进 YAML、不重做 `suga manifest` 或 chat `:model` 的既有语义、不实现交互式配置问答向导。
- expansion_policy：DEFER_OR_REQUEST。若实施中发现必须改变配置优先级、`SUGA_CONFIG` 语义或 provider 协议，停止并回到设计 Gate。
- review budget：中等；初始化涉及用户文件和并发，配置写回涉及注释保留与回滚，必须有隔离单测、PTY 场景和一次反向验证。
- 停止条件：用户批准本设计和对应计划前不实施；任何情况下不得在未确认的路径上覆盖用户配置或泄露密钥。

## 背景与目标

### 当前行为（2026-10-17 核对）

| 现状 | 代码位置 |
|---|---|
| `cmd/suga` 在非 `version`/`--version`/`-V` 命令前先调用 `appconfig.Load()`；加载失败直接打印 `load config:` 并退出，尚无初始化前置阶段。 | `cmd/suga/main.go:23-34` |
| `Load` 根据 `SUGA_HOME` 定位 home，读取 `$SUGA_HOME/.env`，然后读取显式 `SUGA_CONFIG`，否则读取 `$SUGA_HOME/config.yaml`；`.env` 中的 `SUGA_HOME`/`SUGA_CONFIG` 会被恢复。 | `internal/config/load.go:14-45` |
| 默认 home 是 `os.UserHomeDir()/.suga`，也支持 `SUGA_HOME`；当前路径函数只返回路径，不创建目录。 | `internal/config/paths.go:29-49` |
| 当前默认配置样例是带大量注释的完整参考文件，含模型、provider、`compact_model`、`fallback_models`、chat 与 tools 设置；其中 provider 密钥只写 `${...}` 引用。 | `config/config.yaml:1-19,68-75,133-180,223-241` |
| 现有快速开始要求用户手工创建 `~/.suga/config.yaml`，并没有 `init`、`config` 或 `models` 命令。 | `README.md:32-50`、`README.zh-CN.md:32-50`、`internal/cli/root.go:49-63` |
| `version` 单独绕过配置加载，且 CLI 的 `version` 与 root `--version` 都只输出版本；这已经满足“无副作用版本查询”的方向。 | `cmd/suga/main.go:28-34`、`internal/cli/root.go:41-47`、`internal/cli/version.go:10-19` |
| `capp.App` 注册顶层命令，`capp.Cmd.Add` 注册子命令；有子命令的 group 不能声明位置参数，缺少子命令时显示 group help。 | goutil `cflag/capp/app.go:112-189,191-246`、`cflag/capp/cmd.go:134-179,182-215` |
| goutil v0.8.3 已提供 `Cmd.Add`、`Sub`、`HasSubs`、别名和 group help，官方 `subcmd_test.go` 覆盖注册顺序、别名、位置参数冲突和 help。 | `go.mod:7-12`、goutil `cflag/capp/cmd.go:134-235`、`cflag/capp/subcmd_test.go:94-206` |
| `ReadModelSettings` 复用 `ReadProviders`、按 `--model > SUGA_MODEL > model` 选主模型，并解析 compact/fallback；`ReadProviders` 合并三个内置 preset 与用户 `providers.*`。 | `internal/config/providers.go:127-244,348-390,516-567` |
| provider 的 `models` 是可配置模型覆盖，支持 `model_id`、协议、地址、上下文、输出上限、thinking、price；`ContextWindowFor` 和 `ModelSpec.APIModel` 是现有有效值计算入口。 | `internal/config/providers.go:27-49,60-117,278-301,569-590` |
| `suga manifest` 已显示有效主模型、compact、fallback，并保证不请求 provider；它通过 `wiring.LoadManifest` 读取同一套模型设置。 | `internal/cli/manifest.go:15-40,43-62`、`internal/wiring/manifest.go:319-358,400-458` |
| chat `:model` 已使用传入的模型候选做显示、切换与 Tab 补全；候选来源是 `ModelOption`，命令列表和补全在 chat 层。 | `internal/chat/chat.go:33,127-134`、`internal/chat/autocomplete.go:11-49`、`internal/chat/chat_model_test.go:15-67` |
| `goccy/go-yaml` 已是直接依赖，且项目已经用其 parser/AST 读取和写回带注释的 YAML 审批规则；AST 支持路径、注释和 value node replacement。 | `go.mod:7`、`internal/wiring/rules.go:13-14`、`internal/wiring/rules_write.go:10-12`、goccy `ast/ast.go:184-189,1182-1375`、`path.go:330-357,582-813` |
| 现有测试使用 `t.TempDir`/隔离 `SUGA_HOME`，配置断言使用 `github.com/gookit/goutil/x/assert`；配置测试已覆盖 `SUGA_CONFIG`、`.env` 与 Windows 可移植的 `filepath.Join`。 | `internal/config/load_test.go:12-31,46-78,96-127`、`internal/config/paths_test.go:11-34` |

### 用户可见目标

1. 第一次执行会触发初始化的命令时，创建 `SUGA_HOME`（默认 `~/.suga`）和 0700 的最小注释模板 `config.yaml`；不创建 `.env`，只提示它的位置。
2. `suga init` 是明确、可重复执行的初始化入口：已有文件默认不动；`--force` 先生成备份再覆盖；备份失败或写入失败都不改变原文件。
3. `suga config` 能读取生效值和来源，安全地设置/删除配置键，保留注释和用户格式，拒绝密钥写入 YAML。
4. `suga models` 只读本地配置，列出内置 provider 命名空间、用户 `providers.*.models`、主/compact/fallback 标记和 chat `:model` 可切换范围，支持稳定 JSON。
5. `version`、`--version`、全局 `--help`、各命令的 `--help` 没有创建目录、文件、备份或 `.env` 的副作用。

## 范围与非目标

### 范围

- 初始化门控、目录/文件权限、并发首次运行、Windows 路径与 `SUGA_CONFIG` 规则。
- `suga init [--force]`。
- `suga config get/set/unset/list/path`。
- `suga models [--json]`。
- 配置模板、README 中英、`suga-guide` 的首次配置文案和不重复的关系说明。
- 单元测试、CLI 集成测试、Linux PTY 冒烟和反向验证。

### 非目标

- `.env` 自动生成、密钥生成或密钥验证；`.env` 仍由用户自行创建。
- 通过 `models` 请求 OpenAI/Anthropic/网关的模型目录；没有远端模型 API 假设。
- 让 `suga config set` 改写 `SUGA_MODEL` 等环境变量；它只写 YAML，且明确显示环境覆盖。
- 把所有运行时派生状态（session、manifest、工具列表、memory backend）复制为配置键。
- 兼容 `llm:` 旧配置；现有 `CheckLegacyConfig` 的拒绝语义继续有效（`internal/config/providers.go:505-513`）。

## 已确认事实与规范

### 配置来源和优先级

配置命令采用两个层次：原始 YAML 节点用于保留格式和注释；`internal/config` 的现有 typed reader 用于解释和校验。

| 来源 | 语义 | 示例 |
|---|---|---|
| 默认 | 现有 `Read*` 函数的默认值 | `chat.history_size=1000`、`tools.snapshots.keep_days=7` |
| 配置文件 | `$SUGA_HOME/config.yaml`，或显式 `SUGA_CONFIG` 文件 | `model`、`providers.proxy.type` |
| 环境变量 | 现有优先级明确支持的变量；不会凭空增加同名规则 | `SUGA_MODEL` 覆盖 `model`；`${VAR}` 解析为密钥或 URL |
| 命令行临时值 | 只属于运行命令，不纳入 `config get` 的持久写回 | `suga run --model`、`--thinking` |

`config get` 输出“有效值”时遵守上述规则，但来源列只报告 `config`、`env` 或 `default`；命令行没有在 config 子命令中介入。值为 `${VAR}` 时显示脱敏后的有效值和变量名来源，不打印 secret 原文。

### 初始化触发矩阵

| 命令 | 默认初始化 | 原因 |
|---|---:|---|
| `suga chat`、`suga run`、`suga acp` | 是 | 需要运行时配置/数据根 |
| `suga init` | 是（显式） | 只做初始化，不要求已有模型配置 |
| `suga config ...`、`suga models` | 是 | 这些就是上手和配置入口；缺文件时先给用户可编辑模板 |
| `suga manifest`、`trace`、`sessions`、`stats` 等需配置/数据的命令 | 是 | 保持首次执行用户命令的一致行为；命令仍可能因缺模型或无会话报业务错误 |
| `suga version`、`suga --version`、`suga -V`、全局/子命令 `--help` | 否 | 版本与帮助必须是纯查询 |

实现方式是 `cmd/suga` 在解析出命令名后执行一个纯路径预检：帮助和版本早退；其余命令调用 `internal/config.EnsureHome`，然后才进入现有 `Load`。不要在 `ResolveSugaHome` 或包 init 中创建目录，避免测试和库调用产生隐式副作用。

### `SUGA_HOME`、`SUGA_CONFIG`、权限与并发

- 默认路径用 `os.UserHomeDir`；Windows 使用 `filepath.Join` 和 `os.UserHomeDir` 得到 `%USERPROFILE%\\.suga`，显示时使用平台原生分隔符。测试不写真实 home。
- `SUGA_HOME` 非空时先 `filepath.Abs`，作为数据根；创建目录使用 0700（Windows 用 ACL/只读属性能表达的最小等价安全设置，至少不放宽已有权限）。`config.yaml` 和备份使用 0600，保留已有文件模式时不主动放宽。
- 首次创建使用进程内路径锁加跨进程 lock file（位于 `SUGA_HOME/run/locks`；现有会话锁目录在 `internal/cli/gc.go:87-100`），锁内再次检查目录和配置文件；已有文件立即视为成功，不覆盖。两个进程同时首次运行最多一个写模板，另一个输出“已存在/已由其他进程初始化”。
- `SUGA_CONFIG` 非空表示用户选择了外部配置文件：不创建或覆盖 `$SUGA_HOME/config.yaml`，`config path` 仍显示两条路径和 `source=explicit`；为运行时数据创建 `$SUGA_HOME` 仅限命令确实需要数据根的场景。`suga init` 在该模式下默认报告“已指定 SUGA_CONFIG，未写入 home config”，`--force` 也不允许越过该保护；可通过未来明确的 `--config <path>` 扩展，不在本期猜测。
- `SUGA_CONFIG` 指向不存在文件时不静默生成另一个文件：返回明确错误，建议 `suga init --config <path>`（若该扩展被批准）或手工创建；本期的 canonical 入口是 `suga init`。
- 写回采用同目录临时文件、`fsync`、Windows 可替换文件策略和 rename；rename 失败保留原文件，清理临时文件。`set/unset` 的写锁覆盖读、AST 修改、临时写、校验和替换，避免两个进程丢更新。

### 模板来源与 `suga-guide`

选择“仓库模板 + `go:embed`”而不是运行时读取工作目录：新增 `config/onboarding.yaml` 作为最小模板源，CLI 用 `//go:embed` 嵌入；模板只含安全默认和注释，不含真实密钥。`config/config.yaml` 继续是完整参考样例，模板测试保证其关键键和注释约定不漂移。

模板内容约定：

```yaml
# suga first-run configuration; edit this file or use `suga config set`.
# Secrets belong in <SUGA_HOME>/.env; this file contains no secret value.
model: deepseek/deepseek-chat
# providers:
#   my-gateway:
#     type: openai
#     base_url: "${MY_GATEWAY_BASE_URL}"
#     api_key: "${MY_GATEWAY_API_KEY}"
# tools:
#   allow_write: false
```

`model` 是安全的只读/无写权限起点；没有密钥时真正请求仍会失败并给出配置提示。模板生成后 stderr 只提示路径、编辑模型/密钥和 `.env` 路径，不创建 `.env`，例如：`已创建 C:\\Users\\me\\.suga\\config.yaml；请编辑模型并在 C:\\Users\\me\\.suga\\.env 中配置密钥（文件不会自动创建）`。

`suga-guide` 不再复制一套会漂移的完整模板：首次配置步骤改为先运行 `suga init`，然后引用 `config/onboarding.yaml` 产生的内容；完整键表仍以 `references/config.md` 为准，provider 细节仍以 `references/providers.md` 为准。README 快速开始改为命令优先，保留手工 YAML 作为无二进制场景的备选。

## 总体方案

主线一句话：**命令解析先决定是否允许初始化；初始化器安全地产生模板；配置命令在同一份 YAML AST 上做小范围修改并用现有 reader 校验；模型命令只投影现有 `ReadProviders`/`ReadModelSettings` 的本地结果。**

```text
cmd/suga main
  ├─ version/help  ───────────────> 直接输出，无文件系统写入
  └─ 其它命令
       └─ config.EnsureHome(command, SUGA_HOME, SUGA_CONFIG)
            ├─ mkdir 0700 + embedded onboarding.yaml
            ├─ lock + double-check + atomic write/backup
            └─ internal/config.Load()
                 ├─ config file / .env / explicit SUGA_CONFIG
                 └─ existing Read* validation

suga init                 -> initializer
suga config get/set/...   -> source resolver + YAML AST writer
suga models               -> ReadProviders + ReadModelSettings -> local projection
suga manifest             -> existing full capabilities report (不替代 models)
chat :model              -> existing session-local switch/candidates (不改为 config set)
```

## 架构

### 模块与 owner

- `internal/config/bootstrap`（或同包独立文件）：唯一拥有初始化、模板、权限、锁、备份、原子替换和结果类型的深模块；CLI 只渲染结果，不直接 `MkdirAll`/`WriteFile`。
- `internal/config/settings`（或同包）：唯一拥有可写键描述、来源解析、密钥分类和“候选 YAML 加载验证”的适配层。它调用现有 `ReadModelSettings`、`ReadProviders`、`Read*Settings`，不在 CLI 重写 provider/model 规则。
- `internal/cli/init.go`：薄命令适配器，注册 `suga init` 和 `--force`。
- `internal/cli/config.go`：`config` group 及五个子命令；只做参数、输出、退出码。
- `internal/cli/models.go`：本地模型投影与 text/JSON 渲染；模型枚举逻辑归 `internal/config` 或 `internal/wiring`，避免 chat/CLI 各算一遍。
- `internal/wiring`：不新建 provider 连接；若模型投影需要结构，复用 `ReadProviders`/`ReadModelSettings`，不调用 runtime。

### 配置键空间和 set 策略

`set` 采用“已知键 + 受控动态路径”而非任意 YAML 注入：

| 类别 | 允许示例 | 类型/语义校验 | set 行为 |
|---|---|---|---|
| 模型选择 | `model`、`compact_model`、`fallback_models` | string / string list；候选文件经 `ReadModelSettings` 解析 provider、空值和 fallback 下标 | 允许；`model` 可用裸名但最终复用现有解析规则 |
| provider 基本字段 | `providers.<id>.type`、`base_url`、`models.<name>.model_id`、`context_window`、`max_output_tokens`、`thinking`、`prompt_cache` | provider type 用 `ReadProviders`；整数/布尔/枚举走同一 typed reader | 允许；动态 `<id>/<name>` 只接受 YAML 安全标识符 |
| 常用运行时设置 | `workspace`、`max_steps`、`retry.*`、`timeouts.*`、`context.*`、`chat.*`、`tools.*`、`memory.*`、`tasks.*`、`agents.*`、`mcp.*`、`skills.*`、`rules.file` | 先按 YAML 类型解析，再把候选配置交给现有 `ReadFoldSettings`、`ReadChatSettings`、`ReadRetryPolicy`、`ReadSnapshotSettings` 等 reader；未知键拒绝 | 允许已注册 reader 的键；没有 reader 的结构化项必须先补 central descriptor，不在 CLI 偷加解析 |
| 密钥/凭据 | `providers.<id>.api_key`、`serpapi_key`、含 `api_key`/`token`/`password`/`secret` 的键 | 不是“类型不对”，而是写入策略禁止 | 永远拒绝写入 YAML；提示 `.env` 变量名和 `${VAR}` 引用写法，错误信息不回显 value |
| 派生/只读 | `path`、manifest、session、当前 provider 的有效 API key | 无对应配置节点或可能包含敏感值 | `set/unset` 拒绝；由 `get/list/models/manifest` 只读显示 |

`unset` 使用同一 allowlist 和 AST 删除：删除叶节点后清理空的动态 `models.<name>`/provider map，但不删除用户注释；若删除主 `model`，允许文件保存但之后 `ReadModelSettings` 会按现有规则报告未配置。`unset` 不改变环境变量，因此 `get` 仍可能显示 env 覆盖。

类型校验不是复制一份配置语义：

1. descriptor 只声明路径模式、敏感性和现有 reader；
2. `set` 将命令行字符串按声明类型转换为 YAML scalar/sequence（`true/false`、整数、duration、比例、枚举、逗号分隔 list）；
3. 在临时候选文件上重置并加载 `gookit/config/v2`，调用对应现有 `Read*`，特别是 `ReadProviders` 和 `ReadModelSettings`；
4. 校验失败不替换原文件，并把真实键名和期望类型打印出来。

### AST 注释保留和失败回滚

使用现有直接依赖 `goccy/go-yaml` 的 `parser.ParseBytes`/AST，而不是 `map[string]any` marshal。按 YAML path 定位 mapping value，调用 AST value `Replace`；新键插入所在 mapping 的末尾并保留原文注释节点、键顺序、行内注释和换行风格。复杂序列（MCP、headers、rules）默认只允许完整 YAML inline value 语法，不能把 shell 字符串直接拼进 YAML。

写入事务：

1. 打开配置文件并取得跨进程 lock；保存原 bytes、mode、mtime 供诊断。
2. parse AST，解析/修改目标，输出新 bytes；禁止 secret value；用临时路径写 0600，flush + fsync，重新 parse 并运行候选配置 validation。
3. 先将原文件复制为 `<config>.bak.<UTC>.<random>`（0600，`--force` 初始化备份也走同一 helper）；备份失败停止。
4. rename 临时文件到目标；rename/目录 fsync 失败时保留原文件并删除临时文件；若平台替换失败，不能先删目标。
5. 重新读取有效值，输出新值/来源；任何 post-write 检查失败都尝试用保存的原 bytes 原子恢复，并报告“配置未生效/恢复结果”。

备份不包含密钥（YAML 本来就拒绝写入），默认不自动滚动删除历史备份；未来清理另立策略。

## 关键流程

### 首次运行

```text
suga chat
  -> 命令不是 version/help
  -> ResolveSugaHome()
  -> 取得 home init lock
  -> home 缺失：mkdir 0700
  -> config.yaml 缺失且无 SUGA_CONFIG：原子写 embedded onboarding.yaml 0600
  -> 释放锁；stderr 一次性提示 config 和 .env
  -> 现有 Load() 读取配置
  -> chat 继续，缺密钥/模型错误仍按真实配置报错
```

已有目录/文件不被重写；“已存在”不是错误。创建过程任何一步失败都返回错误码 1，并说明目标路径和下一步，不留下半成品。

### `suga init`

```text
suga init                 # 缺失才创建
suga init --force         # 原 config.yaml -> backup；再写模板
```

`--force` 只覆盖 home 下的默认 config；当 `SUGA_CONFIG` 指向外部文件时拒绝覆盖并返回 2。默认 init 不要求模型可用，不发网络请求，不创建 `.env`。成功输出到 stdout，提示性 `.env` 文案到 stderr，便于脚本只消费 stdout。

### `config` 来源显示

```text
$ suga config get model
model = deepseek/deepseek-chat
source = config: C:\Users\me\.suga\config.yaml

$ SUGA_MODEL=openai/gpt-4o suga config get model
model = openai/gpt-4o
source = env:SUGA_MODEL (config is deepseek/deepseek-chat)

$ suga config get tools.allow_write
tools.allow_write = false
source = default
```

`config list` 默认按稳定 key 排序，显示生效值和来源；secret 只显示 `<set>`/`<unset>`，headers/env 中的密钥值也做遮罩。`--json` 不在本期强制加入 config 子命令，避免与任务范围扩大；如实现需要脚本接口，先在 Gate 请求批准。

### `models` 投影

可用模型集合定义为：

1. 三个内置 preset provider `openai`、`anthropic`、`deepseek`，显示为“provider 已知、模型名由用户配置/引用决定”，因为 preset 本身没有本地模型目录也不联网；
2. `providers.<id>.models.<name>` 的每个用户覆盖模型，显示 `model_id`、协议、上下文窗口（通过 `ContextWindowFor` 计算）和 thinking；
3. `model`、`compact_model`、`fallback_models` 当前引用但尚未写在 `models` map 的模型，作为“已引用”条目，避免把有效配置误报为不可用。

不把仅出现在 `model_id` 的重复别名重复显示；规范化 key 为 `<provider>/<model>`，排序按 provider、model。`can_switch=true` 只对当前配置可解析并可由 chat `:model` 候选使用的条目；preset 的任意未列模型不标为可补全候选。

文本示例：

```text
$ suga models
Providers (offline)
  openai       preset; model ids are user-specified
  anthropic    preset; model ids are user-specified
  deepseek     preset; model ids are user-specified
  bailian      custom; 2 configured models

Models
  * bailian/glm-5             current      model_id=glm-5       switch=yes
    bailian/glm-4-flash        compact      context=128000       switch=yes
    openai/gpt-4o              fallback#1   switch=yes
    bailian/sonnet             configured   model_id=claude-sonnet-4-5 switch=yes

: model switch range: bailian/glm-5, bailian/glm-4-flash, openai/gpt-4o, bailian/sonnet
No network request was made.
```

JSON 示例：

```json
{
  "offline": true,
  "providers": [{"id":"openai","kind":"preset","model_ids":[]}],
  "models": [{"ref":"bailian/glm-5","roles":["current"],"can_switch":true}],
  "switchable": ["bailian/glm-5"],
  "source": "config+environment"
}
```

模型错误（缺 provider、空模型、歧义）复用 `ReadModelSettings` 的错误，退出 1；没有网络失败分支。

## 命令接口、输出与退出码

| 命令 | 成功输出 | 失败/退出码 |
|---|---|---|
| `suga init` | stdout `已创建 <config>` 或 `已存在 <config>，未覆盖`；stderr `.env` 提示 | 0；路径/权限/写入失败 1；`SUGA_CONFIG` 保护或参数错误 2 |
| `suga init --force` | stdout `备份 <old> -> <backup>`、`已写入 <config>` | 0；备份失败、替换失败且原文保留 1；保护/参数错误 2 |
| `suga config get <key>` | `key = value`、`source = ...` | 0；未知键/参数 2；配置解析/读取 1 |
| `suga config set <key> <value>` | `已写入 <config>`、`key = ...`、`source = config` | 0；secret、类型、语义校验、并发/写回失败 2（原文件不变） |
| `suga config unset <key>` | `已删除 <key>` | 0；未知/只读/secret 或校验失败 2（原文件不变） |
| `suga config list` | 稳定排序的脱敏键和值及来源 | 0；配置解析 1 |
| `suga config path` | `home`、`config`、`env`、`source` 四行 | 0；路径解析 1 |
| `suga models` | 离线文本列表和 `:model` 范围 | 0；配置无效 1 |
| `suga models --json` | 上述稳定 JSON，stdout 不混日志 | 0；配置无效 1 |

未知命令、缺少位置参数和 goutil parser 错误沿用现有 CLI 默认 1；新增自定义 2 通过现有 `processExitError`/`ExitCode` 机制传递（`internal/cli/errors.go:5-23`）。

## 与现有功能的关系

- `suga manifest` 是完整能力诊断：模型、工具、MCP、skills、memory、输出限制和审批；保持其结构与 `--model/--thinking` 语义，不用 `models` 代替。`models` 是更快的离线模型/配置检查，只输出模型相关内容。
- chat `:model` 是会话级切换：只改变当前/后续会话模型并记录会话事件，不写 `$SUGA_HOME/config.yaml`。`models` 的 `switchable` 必须与 `:model` Tab 候选共享一个 provider-independent 生成函数；不把 `config set model` 当作 `:model` 的实现。
- `suga-guide` 负责解释、排障和安全提示；命令负责确定性执行。guide 不读取或回显 `.env`，只写变量名和路径。
- `config list/get` 可以提示 `suga manifest` 查看完整生效能力，但不能把 manifest 的派生字段纳入 set 键空间。

## 兼容性、安全与风险

- 老用户的现有 `config.yaml`、未知但当前 reader 忽略的字段、注释、键顺序和行内注释都必须原样保留；`set` 只改变目标节点。
- 显式 `SUGA_CONFIG` 永不被 home 初始化覆盖；`config path` 让用户看出当前读的不是 home 文件。
- 并发写、崩溃、磁盘满、Windows rename/占用和 ACL 差异都以 fail-closed 为准，宁可返回错误也不产生半份 YAML。
- 密钥类 key 的检测既按路径（`api_key`、`serpapi_key`）也按保守 token 集合（`token`、`password`、`secret`），误判时宁可提示 `.env`，不把可能的凭据落进 config。
- YAML anchors/aliases、重复 key、未知类型和多文档输入由 parser/validation 拒绝，不自行修复。

## 决策

1. **选 `suga init`，不选 `suga config init`。** 初始化是进程启动前的 home/bootstrap 生命周期，不依赖配置 group；`config` 专注配置读写。`suga init` 更容易在空 home 和无有效 model 时使用，也避免 `config` group 的 help/子命令解析反过来触发配置加载。实现不注册重复 alias，后续若用户要求可另立兼容 issue。
2. **模板独立嵌入，完整样例继续存在。** 这样启动二进制不依赖 cwd/安装目录，最小模板不把完整参考文件的数百个可选键灌入用户配置；二者用测试/文档链接保持一致。
3. **AST 小修补 + 候选文件验证。** 直接 marshal map 会丢注释和格式；直接文本替换不能安全处理 YAML 类型。现有 goccy AST 是最小新增依赖和最接近项目既有规则写回的选择。
4. **模型枚举是本地投影，不是假装 provider 有目录 API。** 内置 preset 只能证明 provider 命名空间存在；具体可切换模型必须来自配置引用或 `providers.*.models`。

## 待确认事项

- Q1：是否接受 `SUGA_CONFIG` 存在时 `suga init` 不写 home `config.yaml`，且缺失外部文件不自动改写路径？建议接受，避免用户以为显式配置被覆盖。
- Q2：是否接受 `config set` 对没有现有 `Read*` 语义校验的复杂键先拒绝，而不是实现一个平行 validator？建议接受，优先保持单一配置语义。
- Q3：`suga models` 对三个内置 provider 显示“任意用户指定模型”而不是列出猜测模型名，是否符合预期？建议接受，保证不联网、不虚构。
- Q4：是否接受 `config set` 默认不提供 `--json`？建议接受；文本已稳定，脚本 JSON 可另立小设计。

## 人工 Gate

用户需要批准：命令选择、初始化触发矩阵、`SUGA_CONFIG` 保护、模板 source-of-truth、密钥拒绝策略、AST/atomic write 方案和 models 的离线枚举语义。批准设计不授权实施；实施必须再批准对应计划。

## 可追溯性

| 需求 | 设计位置 | 后续验证 |
|---|---|---|
| suga-3ck6 首次创建/`init`/force/备份/.env | 初始化触发矩阵、`SUGA_HOME`、首次运行、`suga init` | 隔离 home 单测、CLI 集成、权限/并发/Windows 主机测试 |
| suga-qkoe get/set/unset/list/path | 配置来源、键空间、AST 写回、命令接口 | 注释保留、来源优先级、类型/secret/回滚、goutil 子命令 help |
| suga-iai8 models/offline/json | `models` 投影、关系、输出 | 配置 fixture、JSON schema/稳定排序、无网络断言、chat 候选一致性 |
| version/help 无副作用 | 初始化触发矩阵、架构 | 子进程空 home 的 `version`/`--help` 文件快照断言 |
| 交互与观测 | 范围与 PTY 方案 | Linux `make smoke-chat` 新场景；反向移除初始化/候选逻辑后断言失败 |

## 结论与人工计划 Gate

本设计为 Draft 0.1，当前不实施。用户批准后先按实施计划完成 capability discovery 与基线，再逐功能点提交：初始化、config 子命令、models 命令、文档/PTY/真实环境验证。任何真实 provider 验证只检查“不发模型目录请求”和现有配置解析，不把离线命令误报成 provider 联通验证。
