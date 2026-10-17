<!-- template_id: plan; template_version: 1.2.0 -->
# `suga` 上手、配置初始化与模型列表实施计划

> 状态：Draft 0.3 / 已批准实施

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-17 | suga（gofer job） | 依据 `docs/design/2026-10-17-cli-onboarding-config-design.md` Draft 0.1，拆分初始化、配置 group、模型投影、文档、PTY 与真实环境 Gate。 |
| 0.2 | 2026-10-17 | 监督（Claude） | 跟随设计 Draft 0.2：备份命名（set/unset 单份 `.bak`、init --force 时间戳）、config 写命令 I/O 失败退出 1、run/acp 初始化提示只写 stderr 的测试要求。 |
| 0.3 | 2026-10-17 | 监督（Claude） | 用户批准实施（2026-10-17“.env 自动创建但是不写值，只留简要的注释内容说明使用方式；其他按建议来”）：T1 增加仅注释的 `.env` 模板（已存在永不覆盖）；Q1–Q4 按建议。 |

### 思考模式与范围冻结

- thinking_mode：RIGOROUS。
- core_objective：实现首次运行安全初始化、注释保留的配置维护命令和完全离线的模型列表；保持现有配置优先级、provider 解析、manifest、chat `:model` 和 ACP/run 行为不变。
- allowed_scope：`cmd/suga`、`internal/config`、`internal/cli`、必要的 `internal/wiring`/`internal/chat` 共享模型候选、`config/onboarding.yaml`、README 中英、`skills/suga-guide`、测试和 `scripts/ptysmoke`。
- non_goals：向 `.env` 写入任何值、远端模型目录/网络探测、provider 协议变更、密钥写入 YAML、manifest 重做、chat `:model` 持久配置写回、无批准的 JSON config API。
- expansion_policy：DEFER_OR_REQUEST。改变 `SUGA_CONFIG`、配置优先级、密钥策略、YAML 写回语义或 models 可用集合定义时停止，不在任务中自行扩展。
- review budget：中等；每个功能点原子提交，先近端单测再全量，交互行为用 ptysmoke，不留人工 chat 验证。
- 停止条件：计划未获批准不实施；初始化/写回若无法证明不覆盖用户数据，则停止。

## 目标与完成定义

目标：落地设计稿的三个 issue：`suga-3ck6`、`suga-qkoe`、`suga-iai8`。

完成定义（全部满足）：

1. `suga chat/run/acp/config/models` 等需配置命令在默认或 `SUGA_HOME` 下首次运行时创建 0700 home 与 0600、带注释且无密钥的嵌入模板；已有文件不动；并发只有一次成功写入；`.env` 不创建只提示。
2. `suga init` 默认幂等，`--force` 先备份后覆盖；`SUGA_CONFIG` 明确保护；version/help 在空 home 中无任何创建/写入副作用；Windows 路径和文件替换有主机证据。
3. `suga config get/set/unset/list/path` 用 goutil capp 子命令 API 注册；来源、优先级、脱敏、secret 禁写、类型/语义校验、YAML 注释/顺序/行内注释保留、并发写与失败回滚均有测试。
4. `suga models` 和 `--json` 不构造 provider、不启动 MCP、不发网络请求；列出 preset 命名空间、用户 models、主/compact/fallback 和 `:model` switchable 范围；候选集合与 chat 补全共享。
5. `suga manifest`、`:model`、`suga-guide` 各自职责清楚，没有重复实现或互相覆盖；既有配置单测和 layering test 不回归。
6. Linux 容器完成 `gofmt -l $(git ls-files "*.go")`、`go vet ./...`、`go test -race ./...`、`make smoke-chat`；Windows 主机完成 `go build ./...`、`go vet ./...`、`gofmt -l $(git ls-files "*.go")`、`go test -count=1 ./internal/... ./pkg/...`、`staticcheck ./...`。
7. PTY 新场景先做一次反向验证：临时去掉初始化/候选/来源断言对应逻辑时必须失败；未在 Windows 声称 PTY 通过。
8. 至少用一个真实 provider 跑一次已有工具调用对话，证明本次 CLI 不做网络模型目录请求；若环境或权限不具备，交付明确 `UNVERIFIED_REAL_ENV`，不得用 fake provider 冒充。
9. 每个功能点单独 `docs:`/实现前缀提交；提交前 `git status --short` 与 `git diff --cached --name-only` 只含该功能文件；不改 `.gofer/tracker`、不 push。

## 范围、排除项与授权

- 范围：上述目标、设计稿、测试和文档；只在当前 worktree 实施，不改 worktree 外文件。
- 存储：不增加 SQLite migration；初始化只产生 home/config/.env（仅注释）/backup/lock 等文件；`.env` 已存在永不覆盖。
- 配置：`config set/unset` 只操作显式 YAML 配置源；环境变量保持现有优先级，不被写回。
- 离线：`models` 只读 `$SUGA_HOME`/`SUGA_CONFIG` 和已有本地配置；不发 HTTP，不启动 MCP、hook 或 provider 子进程。
- 交互：PTY 只用于 chat 已有行为和 models/候选共享的回归；CLI config/init 以真实二进制非交互运行覆盖。
- `host_or_non_offline_action=REQUIRED`：Windows 主机 job、真实 provider 工具调用对话和 Linux PTY 容器验证是完成 Gate 的外部环境动作，计划阶段不执行。
- 授权：本计划批准后可在本地实施、运行容器/主机验证并本地提交；push、tag、release、Gofer tracker 变更另行授权。

## 输入与批准证据

- 设计：`docs/design/2026-10-17-cli-onboarding-config-design.md`，Draft 0.1 / 待用户审批。
- issue：`suga-3ck6`（初始化）、`suga-qkoe`（config group）、`suga-iai8`（models）。
- 当前配置入口：`cmd/suga/main.go:23-34`、`internal/config/load.go:14-45`、`internal/config/paths.go:39-49`。
- 当前模型解析：`internal/config/providers.go:127-244,348-390,516-567`。
- 当前 CLI/capp：`internal/cli/root.go:23-65`、goutil `cflag/capp/app.go:191-246`、`cmd.go:134-235`、`subcmd_test.go:94-206`。
- 当前 manifest/chat model：`internal/cli/manifest.go:15-40`、`internal/wiring/manifest.go:319-458`、`internal/chat/autocomplete.go:11-49`、`internal/chat/chat_model_test.go:15-67`。
- 当前 YAML AST precedent：`internal/wiring/rules.go:13-14`、`internal/wiring/rules_write.go:10-12`；依赖声明 `go.mod:7`。
- 实施前重新记录 HEAD、branch、dirty 状态；若代码事实与设计不符，停止进入 W1。

## Capability Discovery

### Capability decisions

| capability_id | required_capability | searched_candidates | direct_reuse | thin_adapter_or_owner_extension | decision | proven_gap | duplication_and_lifecycle_risk |
|---|---|---|---|---|---|---|---|
| CAP-01 | 解析命令并注册 `init/config/models` 子命令 | `internal/cli/root.go:23-65`、goutil `cflag/capp/app.go:191-246`、`cmd.go:134-235`、`subcmd_test.go:94-206` | `capp.NewApp`、`capp.NewCmd`、`Cmd.Add`、`Sub`、group help、现有 `ExitCode` | `internal/cli` 增加 group/leaf adapters；不自建命令树 | DIRECT_REUSE | 当前 root 只有顶层命令，无 config group | 不能在 main 和 cli 各解析一次；group 不声明 positional args |
| CAP-02 | 首次安全创建 home/template/lock/backup | `internal/config/paths.go:39-49`、`internal/cli/gc.go:87-100` 的 locks、`internal/personal/process/lock*.go`、现有 secure dir/spill 约定 | `ResolveSugaHome`、现有跨进程锁、权限 helper 约定 | `internal/config/bootstrap` owner + CLI init 薄适配 | MINIMAL_NEW_MODULE | 当前 paths 只返回路径，main 直接 Load，无模板/备份/并发初始化 | 初始化必须单一 owner；不能让 `config`、`main`、`init` 各写一套 |
| CAP-03 | 配置来源、键策略和现有 reader 校验 | `internal/config/load.go:31-45`、`providers.go:139-244,516-567`、各 `Read*Settings`、`config/references` | `Load`、`ReadProviders`、`ReadModelSettings`、typed readers、`CheckLegacyConfig` | 新 registry 只编排 descriptor/source，不在 CLI 复制 provider 规则 | MINIMAL_NEW_MODULE | 当前没有统一 key descriptor/source attribution | descriptor 只描述路径/敏感性/reader；语义仍以现有 reader 为准 |
| CAP-04 | 注释与格式保留的 YAML 写回 | `go.mod:7`、`internal/wiring/rules_write.go:10-12`、goccy AST `parser.ParseBytes`/`Replace` | 已有 goccy parser/AST 依赖与规则写回模式 | 新 config AST writer；CLI 不接触 AST | MINIMAL_NEW_MODULE | 当前没有 config.yaml writer | 禁止 map marshal；锁、临时文件、fsync、rename 与 restore 只能有一个 owner |
| CAP-05 | 离线模型集合和 chat switchable 候选 | `internal/config/providers.go:432-464,516-567`、`internal/wiring/manifest.go:304-307,330-397`、`internal/chat/autocomplete.go:37-49`、`chat_model_test.go:15-67` | `ReadProviders`、`ReadModelSettings`、`ModelSpec`、现有 ModelOption/补全 | 新 model catalog 生成 canonical refs；manifest/chat/CLI 只适配 | MINIMAL_NEW_MODULE | 当前 models 命令不存在，chat 候选在运行时输入中组装 | 不允许 models 再做一套 loose matching；必须共享候选排序/role 标注 |
| CAP-06 | 初始化/配置/模型的验证与观测 | `internal/config/*_test.go`、`internal/cli/manifest_test.go`、`scripts/ptysmoke/chat_observe.py`、Makefile smoke list | `t.TempDir`、gookit assert、trace JSON、Makefile smoke 模式 | 新 CLI/PTY tests 与已有 helper 接入 | OWNER_EXTENSION | 当前没有 init/config/models 验收和反向场景 | 测试不能写真实 home；PTY 只在 Linux/macOS 跑，Windows 单独 job |

### New module candidates

| candidate_id | capability_id | proposed_module | searched_candidates | direct_reuse_gap | thin_adapter_or_owner_extension_gap | proven_gap | unique_owner_and_lifecycle | deletion_or_merge_handling |
|---|---|---|---|---|---|---|---|---|
| MOD-ON-01 | CAP-02 | `internal/config/bootstrap.go`（或同包拆分文件） | `paths.go`、personal secure file/spill、process locks | 无 init/template/atomic backup/permission API | 可复用路径和锁约定，CLI 只消费 Result | 首次运行确实没有创建逻辑 | home/config 生命周期 owner；`suga init` 与 main 共用 | 删除后退回当前“Load 失败”；不保留第二套自动 init |
| MOD-ON-02 | CAP-03 | `internal/config/settings_registry.go` | 各 `Read*Settings`、`providers.go`、guide config table | 无统一 key/source/secret descriptor | descriptor 调现有 reader；来源 resolver 只做报告 | config get/set 需要可枚举 key 和类型 | 配置语义 owner；CLI 删除后 reader 仍完整 | 若 descriptor 变成第二 validator，合并回现有 reader |
| MOD-ON-03 | CAP-04 | `internal/config/yaml_edit.go` | `internal/wiring/rules_write.go`、goccy `ast`/`path` | 无 config AST targeted write/rollback | 复用 goccy parser/replace 与原子文件 helper | map marshal 会丢用户注释/格式 | 所有 config set/unset 的唯一 writer | 删除后 config set 不可安全实现，不改用 map marshal |
| MOD-ON-04 | CAP-05 | `internal/config/model_catalog.go` | `ReadProviders`/`ReadModelSettings`、manifest model structs、chat ModelOption | 无 preset+configured+role+switchable projection | manifest/chat/models 适配同一 projection | `suga models` 与 `:model` 需要稳定共享集合 | 本地模型 catalog owner，不负责网络/provider | 删除后让 models 调现有 projection；不得复制 matching |

### Rejected new tools

| rejected_candidate | capability_id | deletion_test_and_reason |
|---|---|---|
| `suga config discover-models` 网络工具 | CAP-05 | 删除后本地 `models` 仍满足需求；网络会违反离线和无副作用目标 |
| 模型可调用的 config writer tool | CAP-03 | 删除后 CLI/API 仍可维护配置；避免模型绕过审批/secret 规则写用户 home |
| 独立 YAML 库或 map marshal 层 | CAP-04 | 删除后现有 goccy AST 足够；新增库或重序列化会丢注释/格式 |
| `suga config init` 与 `suga init` 双入口 | CAP-01 | 删除一个可减少冲突；选择 top-level `suga init` 作为不依赖有效配置的 bootstrap |

## Preflight

- P0：记录 `git status --short`、HEAD、分支；确认两个目标文档以外没有 dirty 改动。若有夹带，停止并汇报。
- P1：运行基线 `go test -count=1 ./internal/config/... ./internal/cli/... ./internal/wiring/... ./internal/chat/...`；记录 exit code 和原始输出，不以失败基线进入实现。
- P2：运行 `go doc`/直接读 goutil v0.8.3 `cflag/capp` 的 `Cmd.Add`、`Sub`、group parser 和 `subcmd_test.go`；不要升级 goutil，除非实际 API 缺失并经 Gate 批准。
- P3：确认 `SUGA_CONFIG`、`SUGA_HOME`、`.env` 的现有优先级与测试隔离；所有测试使用 `t.TempDir`，不碰真实 `~/.suga`，不在 repo `tmp/` 留测试副本。
- P4：确认 `goccy/go-yaml` AST parser/Replace 能保留注释、键顺序、行内注释和 CRLF/LF；若实验显示不能完全保持，停止而不是改成 map marshal。
- P5：确定初始化 lock 与已有 `run/locks` 的锁顺序/跨平台实现；并发测试必须启动两个真实调用路径，不只直接调用私有函数。
- P6：config set 的每个新函数都要 grep 实际调用点；测试从 `capp.App.RunWithArgs` 进入真实按键/命令路径，不能只测内部 helper。
- P7：PTY 场景需使用真实二进制 + `PtySession` + `fakellm.py`（只涉及 chat 候选回归时使用现有 mode），新场景加入 `make smoke-chat`；先安排去掉被测逻辑的反向运行。
- P8：先读 `suga-guide` 当前首次配置/配置键文案，再改命令说明；避免把 `.env` 内容回显进测试输出或文档示例。

## 波次与依赖

| 波次 | 任务 | 依赖 | 执行方式 |
|---|---|---|---|
| W0 | T0 基线、API/风险确认 | 无 | 只读；失败停 |
| W1 | T1 初始化与启动门控；T2 goutil config group 骨架 | T0；可并行设计，按 root/main 共享文件串行合入 | 每任务原子提交 |
| W2 | T3 来源/key registry/readers；T4 AST writer/atomic rollback | T1/T2；T3→T4 | 每任务原子提交 |
| W3 | T5 `config get/set/unset/list/path` 完整行为 | T3/T4 | 串行，覆盖真实 CLI |
| W4 | T6 model catalog + `suga models --json`；T7 chat candidate sharing | T3；T6→T7 | 可并行实现，共享候选文件串行合入 |
| W5 | T8 文档/guide/README；T9 CLI/PTY/反向验证 | T1–T7；T8 可与 T9 文档并行，Makefile 串行 | Linux 容器验证 |
| W6 | T10 Windows/全量/真实 provider Gate | T5–T9 | host_or_non_offline_action；不通过不宣布完成 |

## 任务

### T0 基线与 API 确认

- 文件：无产品改动；验证记录在 job/交付报告。
- 动作：确认三个 issue、设计状态、worktree dirty 状态；核对 `capp` 子命令 API；核对 goccy AST 与现有规则写回；运行配置/CLI/wiring/chat 基线。
- 验证命令：`go test -count=1 ./internal/config/... ./internal/cli/... ./internal/wiring/... ./internal/chat/...`；`go doc github.com/gookit/goutil/cflag/capp`。
- 完成标准：API 证据和基线输出可追溯；无 owner 冲突；若基线失败，标记 `BASELINE_FAILED`，不进入 W1。
- 反向验证方法：不适用产品反向验证；基线失败即 fail-closed。
- 预计改动规模：XS，无文件。
- 提交：不提交。

### T1 初始化器、模板与启动门控

- 文件：`cmd/suga/main.go`、`internal/config/paths.go` 或新 `internal/config/bootstrap*.go`、`config/onboarding.yaml`、初始化测试、必要的 `internal/cli/root.go`。
- 动作：加入 `EnsureHome` 结果模型和 embedded template（`config/onboarding.yaml` 与仅注释的 `config/onboarding.env`：说明 `NAME=value` 格式、`${NAME}` 引用、内置 provider 变量名、勿提交版本库，不含任何赋值行；`.env` 缺失才创建 0600，已存在任何情况——含 `init --force`——都不动）；区分 version/help 与需初始化命令；创建 home 0700、config 0600；已有文件不动；进程/跨进程锁 double-check；原子临时写；`SUGA_CONFIG` 外部源保护；错误不留半成品。保留现有 `Load` 的 `.env` 和 config precedence。
- 验证命令：`go test -count=1 ./internal/config/... ./internal/cli/...`；`go build ./...`；空隔离 home 下运行真实二进制 `version`、`--help`、`init`、`config path`。
- 完成标准：首次、已有、force 前置契约、失败恢复、并发、权限、Windows `filepath` 语义有测试；`run --output-format stream-json` 与 `acp` 在空 home 首次运行时 stdout 不混入初始化文字（测试断言）；`version/help` 文件快照为空；模板无密钥且带注释；新函数有 main/CLI 实际调用点。
- 反向验证方法：临时在 version/help 前调用 `EnsureHome`，空 home 的副作用断言必须失败；临时删除 double-check，双进程初始化测试必须发现覆盖/错误；临时启用 `SUGA_CONFIG` 时仍写 home，保护测试必须失败。
- 预计改动规模：M/L，4–8 个 Go/YAML/测试文件。
- 依赖/并行：T0 后；T2 可在不同文件实现，但 root/main 合入按 T1→T2。

### T2 `suga init` 与 capp `config/models` 注册骨架

- 文件：`internal/cli/root.go`、新 `internal/cli/init.go`、`config.go`、`models.go`、CLI registration tests。
- 动作：使用 goutil v0.8.3 `capp.NewCmd`/`Cmd.Add` 注册 top-level `init`、`config` group（暂含 leaf placeholders）、`models`；`config` group 不声明 positional args；接入 `--force`；help 文案和 `root.LongHelp` 更新；错误通过既有 `ExitCode`。
- 验证命令：`go test -count=1 ./internal/cli/...`；`go run ./cmd/suga --help`；`go run ./cmd/suga config --help`；`go run ./cmd/suga config get --help`（使用隔离环境，避免真实 home）。
- 完成标准：help 列出 subcommands、别名/位置参数没有破坏；`suga config` 无子命令显示 group help，不触发写；实际 `RunWithArgs` 可到 leaf；不改变 version 行为。
- 反向验证方法：临时把 `config` 注册成 leaf 或给 group 加 positional arg，subcmd/group help 测试必须失败；临时从 root 去掉注册，registration test 必须失败。
- 预计改动规模：M，4–7 个 Go/测试文件。
- 依赖/并行：T1 后；T3 依赖 key group 可调用，T6 依赖 models command。

### T3 配置来源、键 registry 与复用现有 readers

- 文件：`internal/config` 新 registry/source 文件及测试；必要时扩展现有 `Read*` 导出最小适配；不修改 provider 语义。
- 动作：定义 known key/path matcher、类型转换、secret classifier、来源 resolver 和 reader dispatcher；实现 `model` 的 `SUGA_MODEL` precedence、`${VAR}` 变量名来源、默认值来源；允许 provider/model 动态键但拒绝未知结构；调用现有 `ReadProviders`、`ReadModelSettings`、`ReadChatSettings`、`ReadRetryPolicy`、`ReadSnapshotSettings` 等，不复制校验。
- 验证命令：`go test -count=1 ./internal/config/...`。
- 完成标准：每一类键有 type/source/secret 表驱动测试；无 secret 原文输出；错误含键名和期望类型；配置无效仍与现有 startup reader 一致；未使用 reader 的键不伪造“已校验”。
- 反向验证方法：临时让 registry 用独立宽松 parser 接受错误 bool/provider type，现有 reader 负例必须失败；临时忽略 `SUGA_MODEL`，来源 precedence 测试必须失败；临时关闭 secret classifier，secret-write guard 测试必须失败。
- 预计改动规模：L，4–8 个 Go/测试文件。
- 依赖/并行：T1/T2 后；T4 依赖 descriptor/source contract。

### T4 注释保留的 AST writer、原子替换与回滚

- 文件：`internal/config` 新 `yaml_edit`/atomic write 文件及测试；复用 `internal/wiring/rules_write.go` 的 goccy 风格但不改规则文件语义。
- 动作：用 goccy parser AST 定位 path，替换/插入/删除 value；保留注释、顺序、行内注释和换行；临时文件 + fsync + 0600 + rename；配置 lock 覆盖 read-modify-write；写前备份 helper；候选文件 reparse + reader validation；失败保留原文件并清理 temp，恢复失败明确报告。
- 验证命令：`go test -count=1 ./internal/config/...`；针对 CRLF/LF、重复 key、anchor、嵌套 map/list、磁盘写失败的定向 tests。
- 完成标准：`model` scalar、fallback list、nested provider/model、unset、comments/inline comments/key order/line endings 都有断言；故意注入 parse/type/rename/validation failure 后原 bytes hash 不变；所有写在 lock 内。
- 反向验证方法：临时改用 `map[string]any` marshal，注释/顺序测试必须失败；临时先 truncate 目标再 rename，注入 rename failure 的原文保护测试必须失败；临时去掉 post-write validation，坏 provider fixture 必须失败。
- 预计改动规模：L，3–7 个 Go/测试文件。
- 依赖/并行：T3 后；T5 依赖 writer API。

### T5 完成 `suga config get/set/unset/list/path`

- 文件：`internal/cli/config.go`、config CLI tests、必要的 `internal/config` adapter tests、root help。
- 动作：实现五个 leaf：
  - `get <key>` 输出生效值和来源，secret 脱敏；
  - `set <key> <value>` 类型转换、secret 拒绝、AST write/backup/validation；
  - `unset <key>` 删除配置叶节点，不碰环境变量；
  - `list` 稳定排序、生效值/来源、密钥 mask；
  - `path` 输出 home/config/.env/source；
  所有命令在隔离 home/explicit config 下走真实 capp app。
- 验证命令：`go test -count=1 ./internal/config/... ./internal/cli/...`；隔离 home 中执行 `go run ./cmd/suga config get/set/unset/list/path`；检查 stdout/stderr 分离和退出码。
- 完成标准：设计中的示例输出/退出码可追溯（输入错误 2、I/O 失败 1）；连续多次 `set` 只留一份 `config.yaml.bak`；config 注释保持；环境覆盖可见；secret/API key 不落 YAML；并发和失败不丢更新；未知键、只读/derived 键、坏类型返回 2；`.env` 从不自动创建。
- 反向验证方法：临时把 `set providers.x.api_key` 写入 AST，secret test 必须失败；临时把 `get model` 只读 raw config，SUGA_MODEL precedence test 必须失败；临时绕过 AST writer，comment preservation test 必须失败；临时让失败写先覆盖目标，rollback hash test 必须失败。
- 预计改动规模：L，4–9 个 Go/测试文件。
- 依赖/并行：T3/T4 后；T7 可在 model catalog 后实施，最终 CLI suite 串行。

### T6 model catalog 与 `suga models --json`

- 文件：`internal/config/model_catalog.go`（或 `internal/wiring` 的最小 projection owner）、`internal/cli/models.go`、tests。
- 动作：从 `ReadProviders` 和 `ReadModelSettings` 构造 preset/provider/model entries；加入 role 标记 current/compact/fallback；把未写 models map 但被引用的模型保留；用 `ContextWindowFor`/`APIModel` 显示有效本地信息；生成 deterministic `switchable` refs；text 和 JSON 都注明 offline；不建 runtime/provider/MCP。
- 验证命令：`go test -count=1 ./internal/config/... ./internal/cli/... ./internal/wiring/...`；`go run ./cmd/suga models --json`；设置一个会拒绝网络的 `base_url`，确认命令不产生请求。
- 完成标准：preset、custom models、model/compact/fallback、duplicate model_id、ambiguous/invalid config、empty list、sort、JSON key/shape 有测试；无 socket/HTTP/MCP/hook 进程；与 manifest 的当前模型一致。
- 反向验证方法：临时让 models 创建 provider/请求 endpoint，offline test 的 request counter 必须失败；临时按 map iteration 输出，sort snapshot 必须失败；临时丢掉只被引用模型，role/catalog test 必须失败。
- 预计改动规模：M/L，4–8 个 Go/测试文件。
- 依赖/并行：T3 后；T7 依赖 canonical switchable refs。

### T7 与 chat `:model` 候选共享

- 文件：`internal/config/model_catalog.go` 或 `internal/chat` adapter、`internal/chat/autocomplete.go`、模型构建调用方、tests。
- 动作：让 `suga models` 的 canonical switchable refs 与 chat `ModelOption`/Tab 候选来自同一排序/去重投影；保持 `:model` 会话级切换、事件和 fallback 语义不变；不让 chat `:model` 写 config。
- 验证命令：`go test -count=1 ./internal/chat/... ./internal/config/... ./internal/cli/...`；Linux PTY 定向场景运行 `chat_model.py`（完整 smoke 在 T9）。
- 完成标准：chat 显示和补全包含与 `models` 相同的 refs；当前项标记一致；未配置/歧义行为与现有 reader 一致；`:model` 不改变 config file bytes。
- 反向验证方法：临时保留 chat 自己的旧候选生成，fixture 中插入动态 provider/model，cross-output equality test 必须失败；临时在 `:model` callback 写 config，file hash assertion 必须失败。
- 预计改动规模：M，3–7 个 Go/测试文件。
- 依赖/并行：T6 后；T9 需要本任务的 PTY 断言。

### T8 文档、模板和 skill/README 对齐

- 文件：`config/onboarding.yaml`（若 T1 未完全落地）、`README.md`、`README.zh-CN.md`、`skills/suga-guide/SKILL.md`、`skills/suga-guide/references/config.md`、`references/providers.md`、必要的 docs usage；不改已发布设计语义。
- 动作：快速开始改为 `suga init` → 编辑 `.env` → `suga config get/path` → `suga models`/`manifest`；说明 Windows 路径、SUGA_CONFIG、secret 禁写、注释保留、退出码；guide 解释命令与手工备选，避免复制会漂移的完整模板；说明 `models` 离线且不替代 manifest/`:model`。
- 验证命令：文档链接/命令 grep；`python -I ... validate_document.py --kind design ...` 与 `--kind plan ...` 只针对设计/计划；执行隔离二进制 quick-start 命令。
- 完成标准：文档所有现状描述有代码位置或明确“设计”；中英快速开始不矛盾；不出现真实密钥；命令输出/退出码与实现一致；模板关键注释可测试。
- 反向验证方法：临时从 guide 删除 `.env`/SUGA_CONFIG 安全说明，文档 checklist/grep 必须失败；临时把 README 的 `:model` 写回 config 描述改错，doc test 必须失败。
- 预计改动规模：M，6–10 个文档/配置文件。
- 依赖/并行：T1、T5、T6/T7 的最终输出确定后；本计划实施阶段不提前提交这些文档。

### T9 PTY 冒烟、反向验证与 trace/副作用观测

- 文件：`scripts/ptysmoke/chat_onboarding.py`（新，若需要）、`scripts/ptysmoke/README.md`、`Makefile`、必要的 `fakellm.py`；新增 CLI tests 不替代 PTY。
- 动作：真实二进制启动空隔离 home，检查 `version`/`--help` 不创建；用非交互 `init/config/models` 检查文件/输出/退出码；chat 场景检查模型候选与 `:model` Tab/显示一致、`:model` 不改 config；若有 hook/trace 事件，结束后用 `suga trace --json` 检查，不用人工判断。先正式去掉初始化或候选共享逻辑反向运行，确认断言失败，再恢复并纳入 `make smoke-chat`。
- 验证命令：Linux/macOS `python3 -I scripts/ptysmoke/chat_onboarding.py --bin tmp/smoke/suga --work tmp/smoke-onboarding`；`make smoke-chat`；`python3 -I ...` 反向运行命令按脚本 docstring。
- 完成标准：PTY 场景使用 `PtySession` 驱动真实 binary/fakellm；覆盖空 home、已有配置不覆盖、`.env` 提示、`:model` 候选、trace/文件快照；反向验证确实失败；脚本加入 Makefile；Windows 明确 `UNVERIFIED_PTY_WINDOWS`，不冒充通过。
- 反向验证方法：见动作；最低要求移除 `EnsureHome`、去掉 canonical catalog 或绕过 `config set` secret guard 各跑一轮，相关检查必须失败。
- 预计改动规模：M，3–6 个 Python/Make/README 文件。
- 依赖/并行：T1、T5、T7 后；T10 前必须完成。

### T10 Windows、真实 provider 与全量回归

- 文件：无新功能文件；修复只允许回到所属功能点独立提交。
- 动作：在 Linux 容器跑全量 race/vet/gofmt/smoke；通过 gofer exec job 在 Windows 主机跑 build/vet/gofmt/target tests/staticcheck；使用已有真实配置，以一个真实 provider 跑一轮带工具调用对话，确认新 commands 不请求 model list，检查 trace/文件；不能执行则记录 `UNVERIFIED_REAL_ENV`。
- 验证命令：
  - 容器：`gofmt -l $(git ls-files "*.go")`、`go vet ./...`、`go test -race ./...`、`make smoke-chat`；
  - Windows：`go build ./...`、`go vet ./...`、`gofmt -l $(git ls-files "*.go")`、`go test -count=1 ./internal/... ./pkg/...`、`staticcheck ./...`；
  - 真实 provider：带至少一次现有可写工具调用的 `suga chat`/`run`，之后 `suga trace <sid> --json`；不允许把 fake LLM 当真实 provider。
- 完成标准：所有命令同时 exit code=0 且输出无新告警；Windows 证据包含路径/rename/权限/并发相关用例；PTY 只标 Linux/macOS；真实验证有 provider/session/trace 证据或明确未验证标签；`git status --short` 无夹带。
- 反向验证方法：不新增业务反向逻辑；以 T1/T4/T5/T6/T9 的反向记录作为 Gate，任一反向检查不失败即阻止完成。
- 预计改动规模：XS，无产品改动。
- 依赖/并行：T1–T9 全部完成。

## 提交与合并顺序

本计划获批准后按功能点本地提交，不 push：

1. `feat(cli): initialize suga home and onboarding config`（T1）
2. `feat(cli): add init config and models command tree`（T2）
3. `feat(config): centralize config sources and writable keys`（T3）
4. `feat(config): preserve yaml comments during config writes`（T4）
5. `feat(cli): implement config get set unset list path`（T5）
6. `feat(cli): add offline models catalog`（T6）
7. `feat(chat): share model switch candidates`（T7）
8. `docs: document onboarding and configuration commands`（T8）
9. `test(smoke): cover onboarding and offline model commands`（T9）

每次提交前：`git status --short`；`git diff --stat`；`git diff --cached --name-only`；只 `git add` 本任务文件；不改 `.gofer/tracker`。T10 是验证 Gate，不单独提交，若需修复回到对应功能点形成新原子提交。

## 回滚与恢复

- T1 可独立 revert；若删除初始化代码，旧行为是缺 config 时 `Load` 失败，不删除用户已有文件。
- T3/T4/T5 必须按逆序 revert；旧 config YAML 保留，备份不由回滚命令删除；任何写失败不覆盖原文。
- T6/T7 revert 后现有 manifest/`:model` 保持可用；删除 models command 不改变 provider runtime。
- T8/T9 文档/脚本可独立 revert；不要通过删除测试来隐藏行为变化。
- 运行时回退策略：`SUGA_CONFIG` 外部文件和 `$SUGA_HOME/config.yaml` 都不自动迁移；新版本只新增备份/lock 文件，旧二进制忽略它们。若 rename/lock 在 Windows 失败，返回错误而不是降级为 truncate。
- 用户数据恢复：`config set/unset` 后用 `<config>.bak` 恢复，`init --force` 后用 `<config>.bak.<UTC>` 恢复；恢复也必须原子替换并保留原文件副本。`.env` 始终由用户自行管理，本计划不改它。

## 人工 Gate

1. 用户批准设计与本 Draft 0.1 计划，尤其是 `suga init` 而非 `suga config init`、`SUGA_CONFIG` 保护、模板内容和 secret 禁写。
2. T1 完成后核对空 home、已有文件、force 备份、并发和 version/help 无副作用；不通过不进入 T3。
3. T4 完成后核对 AST 注释/格式保留、atomic rollback 和跨进程锁；不通过不得实现 destructive `set/unset`。
4. T6/T7 完成后核对 `models`、manifest、`:model` 的职责与候选一致性；不得用网络探测补齐 preset 模型。
5. T9 完成后审阅 PTY 场景和反向验证；chat 行为不留给人工验证。
6. T10 的 Windows job、Linux PTY 和真实 provider 是外部环境 Gate；无法执行必须写 `UNVERIFIED_*`，不能写成通过。
7. push、tag、release、tracker issue/plan 状态更新不在本计划授权内。

## 可追溯性

| 设计/issue 条目 | 任务 | 验证 |
|---|---|---|
| suga-3ck6 首次 home/config、权限、并发、force/backup、.env 提示 | T1、T2、T8、T9、T10 | 隔离 home/CLI/并发/权限/Windows；PTY 空 home；version/help snapshot |
| `suga init` 选择与生命周期门控 | T1、T2 | capp registration、help、SUGA_CONFIG guard、反向副作用测试 |
| suga-qkoe `config` group/API | T2、T3、T5 | capp subcmd help/真实 RunWithArgs/退出码 |
| get 来源/优先级/脱敏/list/path | T3、T5、T8 | config/env/default fixture、secret mask、stdout/stderr |
| set/unset 类型校验、secret 禁写、注释保留、回滚 | T3、T4、T5 | reader validation、AST comment/order/CRLF、failure hash、concurrency |
| suga-iai8 offline models/text/json | T6、T8、T9、T10 | request counter/no network、JSON/sort/role fixture、真实 binary |
| manifest/`:model`/guide 不重复 | T6、T7、T8 | manifest regression、candidate equality、guide/README checklist |
| PTY/trace/反向验证 | T7、T9、T10 | `make smoke-chat`、`suga trace --json`、remove-logic negative runs |
| Windows/真实 provider | T1、T4、T5、T10 | gofer host job；真实 provider 工具调用；未执行明确标记 |

## 待确认/风险

- goutil v0.8.3 的 group parser 只在第一个位置参数处分派；若 `config` 的 option/argument 设计违反 `Cmd.Add` 约束，必须调整命令表而不是绕过 capp。
- goccy AST 的具体插入 API需要 T4 通过最小实验确认；如果只能保留注释但不能保留全部格式，应停在 T4 Gate，不接受重序列化替代。
- `gookit/config/v2` 是全局状态；T3/T5 测试必须每例 `Reset`/cleanup，候选文件 validation 不能污染后续命令。
- preset provider 没有本地模型目录；models 只能列命名空间和配置引用，不能把常见模型名猜成可切换模型。
- Windows 正在运行的 suga 可能占用 binary，但本计划只写 config 文件；文件被其它程序占用时必须返回明确错误并保留原文。
- 真实 provider 工具调用可能受当前密钥/网络/审批配置影响；不因无法联网修改协议或放宽 secret/approval 规则。

## 完成 Gate

- T0–T10 依赖任务完成；每个功能点有原子提交，`git status --short` 无夹带，`git diff --check` 无输出。
- 设计中的初始化矩阵、`SUGA_CONFIG` 保护、secret 禁写、AST 保留、离线 models、manifest/`:model` 关系均有代码/测试/文档证据。
- 容器 `gofmt`、`go vet`、`go test -race`、`make smoke-chat`；Windows `go build`、`go vet`、`gofmt`、目标 `go test`、`staticcheck` 均记录 exit code 和原始输出。
- PTY 场景已加入 Makefile 并完成反向验证；Windows PTY 不运行则明确写明待监督 Linux 容器验证。
- 真实 provider 工具调用已核对 session/trace，或交付报告明确 `UNVERIFIED_REAL_ENV`；fake LLM 结果不冒充真实验证。
- 用户批准前不实施；完成后只汇报本地提交、验证证据、未验证环境和“发现但不碰”的范围外问题。
