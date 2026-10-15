<!-- template_id: design; template_version: 1.1.1 -->
# Suag 配置重设计、分层整理与 chat 显示 设计

> 状态：Draft 0.2 / 已批准进入计划（2026-10-09 用户：“可以 按推荐来”，Q1–Q3 均按推荐）

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-09 | Claude | 初稿 |
| 0.2 | 2026-10-09 | Claude | 按合并评审修订：只读模式新增 `tools.read_only_allow`（原 CORE_BLOCKING）；补全 hooks 调用协议、投递来源与 injected 规则、Gofer 迁移清单；装配包改名 `internal/wiring`、ACP 工厂放 cli 层、liveText 归 output；补测试迁移原则、Claude SSE 事件与 base_url 规则、模型引用解析规则 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

## 背景与目标

文档思考模式：RIGOROUS。核心目标：让 suag 的配置、代码结构和终端体验达到"可长期维护、对任何调用方中立"的状态。scope freeze：只覆盖下列四个 issue；review budget：一轮合并轴评审（低暴露度，SR1409）；停止条件：评审无 CORE_BLOCKING 且待确认项由用户决定。

用户可见结果：

1. **配置**（suag-kai3）：一个配置文件里定义多个 provider，用 `model: <provider>/<model>` 选默认模型；`--model` 同样写法切换。不再有"单个 `llm` 段 + `--provider` 覆盖时丢弃 key/base_url"的规则。
2. **中立**（suag-kai3 并入）：代码里不再写死 gofer。hooks、默认 MCP 放行、job 提示词、回复前缀都由配置或调用方参数给出；Gofer 通过自己的 agent 配置和用户的 suag 配置启用这些配合。
3. **分层**（suag-2zbi）：`cmd/suag` 只做装配；每个命令一处定义；chat 循环、hooks、运行时装配、输出格式各有归属包；行为不变。
4. **chat 显示**（suag-c7co + suag-l5i7 剩余）：chat 用 gookit/cliui 区分用户输入、模型回复、工具调用、计划、错误，每轮结束显示一行状态；Claude provider 也真正流式输出并支持 `base_url`。

## 名词

- **provider 条目**：`providers.<id>` 下的一组连接参数（协议类型、base_url、密钥、请求头、模型元数据）。
- **模型引用**：`<provider-id>/<model-id>`，如 `opencode-go/deepseek-v4-flash`。
- **hooks 命令**：chat 在会话开始、提交输入、工具结束、回合结束时调用的外部命令，事件为 Claude Code 兼容 JSON（现有协议不变）。

## 范围与非目标

范围：suag 仓库内的配置读取与样例、`cmd/suag` 与 `internal/app` 的重组、chat 渲染、Claude provider 流式与 base_url、相关文档（README、`docs/usage/*`、config 样例）。

非目标：

- 不改会话存储、stream-json 与 ACP 协议字段（golden 契约测试保持不变）。
- 不改 Gofer 仓库；需要 Gofer 侧调整的配置以"迁移说明"交给 gofer 会话。
- 不做多模型路由、按任务自动选模型、provider 的 `models.list` 在线发现。
- `suag-iw3u` 的遗留小项不在本批（Claude thinking 回传仍 deferred：suag 未开启 Claude extended thinking）。

## 已确认事实与规范

- 当前配置键（代码中全部读取点）：`llm.{api_type,provider_name,model,api_key,base_url,compact_model,context_window,headers}`、`max_steps`、`workspace`、`serpapi_key`、`mcp.servers`、`tools.{deferred,auto_allow}`、`transcripts`、`gofer.{hooks,command,agent,wait_sec,timeout_ms}`；env：`SUAG_MODEL`、`SUAG_PROVIDER`、`SUAG_APPEND_SYSTEM_PROMPT`、`SUAG_GOFER_HOOKS`（测试也用它关闭 hooks）、`GOFER_JOB_ID`，以及 `SUAG_HOME`/`SUAG_CONFIG`/`SUAG_DEBUG`。
- `--read-only` 下 MCP 放行只用内置 `defaultMCPAutoAllow`，用户的 `tools.auto_allow` 被忽略（`cli_mcp.go` `newCLIMCPGate`）；Gofer CLI job 使用 `read_only_args: [--read-only]`。
- hooks 调用为 `gofer hook generic --agent <agent>`，Stop 事件追加 `--wait N`；事件除 Claude Code 兼容的 SessionStart/UserPromptSubmit/PostToolUse/Stop/SessionEnd 外，还有 suag 自定义的 Interrupt；PostToolUse 只对 shell/exec 类工具发送，工具名归一为 `shell`；`gofer.hooks: auto` 时 PATH 中有 gofer 即启用。
- `[gofer web 回复]` 前缀由 Gofer 自己加在投递文本上；suag 只做识别：投递时已带该前缀就不再加 `[来自 <from>] `（`suag send` 固定 `from=cli`，缺省来源 `unknown`），run 模式据该前缀把 transcript 中的用户输入标记为 injected。
- gofer 写死行为四处：`internal/app/gofer_hooks.go`（调用 `gofer hook`）、`internal/app/cli_mcp.go` 的 `defaultMCPAutoAllow`、`internal/personal/runner.go` 的 GOFER_JOB_ID 提示词、`[gofer web 回复]` 前缀（`chat.go`/`chat_input.go`/`runner.go`）。
- 规模：`cmd/suag` 2 个文件 537 行，其中直接定义了 acp/version/ps/send/trace 命令和 `runTrace`；`internal/app` 8 个文件约 1900 行，混合了命令定义（`NewPersonalCommand`）、运行时装配（`openPersonalRuntimeConfigured`）、stream-json 输出、chat 循环、hooks、MCP 审批。
- Claude provider 使用固定 `claudeAPIBase`，不读 `base_url`；`Stream` 先 `Generate` 再一次性输出。OpenAI 兼容 provider 已在 1bf358c 改为 SSE 真流式。
- OpenCode Go 的 Qwen/MiniMax/Claude Haiku 模型走 Anthropic `/v1/messages`，需要 Anthropic 协议 + 自定义 base_url。
- 规范：SR1107/SR1207（design→plan→实施）、SR1409（低暴露度一轮合并评审）、SR1403（最小充分）、GR104（单文件 ≤1000 行）。

## 总体方案

### 1. 配置格式

保留 YAML 与 gookit/config（`${VAR}` 展开、`SUAG_HOME/.env` 不变）。参考 opencode 的 `provider/model` 引用和 codex 的 `model_providers`：

```yaml
model: opencode-go/deepseek-v4-flash      # 默认模型引用（必填）
compact_model: opencode-go/glm-5.3-flash  # 可选：摘要用模型引用
fallback_models: [deepseek/deepseek-chat] # 可选：按序备用

providers:
  opencode-go:
    type: openai                          # 协议：openai | anthropic
    base_url: https://opencode.ai/zen/go/v1
    api_key: ${OPENCODE_API_KEY}
    headers:
      x-opencode-session: "<会话 ID 占位符>"  # 现有 headers 占位符语法不变
    models:                               # 可选：模型元数据
      deepseek-v4-flash: {context_window: 128000}
  opencode-go-anthropic:
    type: anthropic
    base_url: https://opencode.ai/zen/go
    api_key: ${OPENCODE_API_KEY}
  deepseek:
    type: openai
    base_url: https://api.deepseek.com/v1
    api_key: ${DEEPSEEK_API_KEY}
```

规则：

- 内置三个 provider 预设 `openai`、`anthropic`、`deepseek`（默认 base_url 与密钥环境变量 `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` / `DEEPSEEK_API_KEY`）；用户同名条目逐字段覆盖预设。其他 provider 必须写 `type`。
- 模型引用解析：只按第一个 `/` 切分，且前缀必须是已定义的 provider（预设或 `providers` 中的条目），否则整串都是 model id、provider 取默认模型的 provider（model id 自身可含 `/`）。`--model`、`SUAG_MODEL`、`compact_model`、`fallback_models` 都按此规则。优先级：flag > env > 配置。
- base_url 拼接：`type: openai` 拼 `/chat/completions`；`type: anthropic` 拼 `/v1/messages`，若 base_url 已以 `/v1` 结尾则只拼 `/messages`。预设 `anthropic` 的 base_url 为 `https://api.anthropic.com`。
- 删除 `--provider`、`SUAG_PROVIDER`、`llm.*` 整段；读到旧 `llm` 段时启动报错并给出新写法提示（不自动迁移，见待确认 Q1）。
- `context_window`：`providers.<id>.models.<model>.context_window` > 内置按模型名推断；合并现有两处推断逻辑（`config.ReadContextWindow` 与 app 的 `contextWindowForSettings`）为一处。
- provider 名即配置中的 id：原 `claude` 标签变为 `anthropic`（或用户自定义 id），trace/stats 按 provider 名的历史统计会出现新旧两个名字；启用备用模型后，会话元数据只记录主模型。
- 其余顶层键保留：`max_steps`、`workspace`、`serpapi_key`、`mcp.servers`、`tools.{deferred,auto_allow}`、`transcripts`；新增 `tools.read_only_allow`（见下节）。

### 2. gofer 行为改为通用配置

| 现状 | 新做法 | Gofer 侧配合 |
|---|---|---|
| 默认放行 `gofer/...` 查询工具；`--read-only` 只用内置名单 | 内置名单清空。新增 `tools.read_only_allow`：只读模式唯一生效的放行名单，条目必须是 `server/toolglob` 限定写法；非只读模式仍用 `tools.auto_allow`。两者缺省为空 | 用户 suag 配置写 `tools.read_only_allow: [gofer/gofer_*_list, gofer/gofer_*_get, gofer/gofer_work_report, gofer/gofer_session_list]`（样例给出） |
| chat 自动调用 `gofer hook generic --agent suag`，`gofer.*` 配置，auto 模式 | `hooks.command`（argv 数组，不经 shell）、`hooks.timeout_ms`、`hooks.stop_args`（只追加到 Stop 事件，如 `[--wait, "7140"]`，其中秒数同时作为本端等待上限）、`hooks.post_tool_use: {tools: [shell_exec, ...], name: shell}`（发送范围与改名）；事件集合不变（含自定义 Interrupt）。未配置即关闭，取消 auto 模式；`SUAG_HOOKS=off` 临时关闭（替代测试用的 `SUAG_GOFER_HOOKS`） | 用户 suag 配置写 `hooks.command: [gofer, hook, generic, --agent, suag]` 与 `stop_args`；Gofer 拉起的 job 设 `SUAG_HOOKS=off`（替代 suag 对 `GOFER_JOB_ID` 的判断） |
| `GOFER_JOB_ID` 时追加汇报提示词（run、chat、ACP 共用装配） | 删除；提示由调用方给出 | Gofer 在 job 环境设 `SUAG_APPEND_SYSTEM_PROMPT`（覆盖 chat/resume/ACP 等不带 `--append-system-prompt` 的路径），run 路径也可继续用 system_prompt |
| 识别 `[gofer web 回复]` 前缀 | 投递：`suag send --source <label>`（缺省 `cli`）加 `[来自 <label>] ` 前缀；文本已以 `[…] ` 形式的来源标签开头时不再加（通用规则，覆盖 Gofer 自带前缀，缺省来源 `unknown` 不变）。run：新增 `--injected` 标记本轮输入非人工键入，替代前缀识别 | Gofer 投递 web 回复时可不改；经 run 续接投递 web 回复时加 `--injected` |

`internal/personal` 不再出现 gofer 字样；`injected` 判定只依据调用方传入的 `Injected` 标志。

### 3. 分层

目标结构（只移动与拆分，不改行为）：

```text
cmd/suag/main.go            装配：加载配置 → 构建命令树 → 运行 → exitReport
internal/cli/               命令定义：每个命令一个文件（run chat acp ps send trace stats export version），
                            只做 flag 解析、调用下层、选择输出格式
internal/wiring/            运行时装配：由配置构建 provider/pool、工具、MCP、审批、Runner（原 openPersonalRuntimeConfigured 等）；不用 runtime 命名以免遮蔽标准库
internal/output/            输出：stream-json 编码、文本实时输出（liveText，run 与 chat 共用）、usage 形状
internal/chat/              chat 循环、输入源、投递队列、渲染（cliui）
internal/hooks/             通用 hooks 客户端（原 gofer_hooks.go）
internal/config/            新配置模型与读取
internal/personal、platform、acpserver、capability   保持
```

`internal/app` 拆完后删除。依赖方向：`cli → wiring/chat/output/hooks/acpserver`，`chat → output/hooks/personal`，`wiring → config/personal/platform/capability`；ACP 的会话工厂闭包放在 cli 层（cli 同时依赖 acpserver 与 wiring），acpserver 不 import wiring，避免循环。

测试迁移原则：测试随被测代码移动并保持内部测试（同包）；跨包测试（如 `stream_json_contract_test`、`chat_hooks_test`）按主要被测对象归属，必要的测试接缝以最小导出（或 `export_test.go`）提供；分层阶段的验收是 golden 契约测试与全量 `go test -race` 结果不变。

### 4. chat 显示与 Claude 流式

- 渲染器（`internal/chat/render.go`）基于 gookit/cliui 与 gookit/color：用户提示符着色；模型文本实时打印；工具调用显示为一行 `⏺ file_read path=...`，结果摘要一行（超长截断）；`update_plan` 显示勾选列表；错误红色；每轮结束一行灰色状态 `model · in/out tokens · ctx 比例 · 耗时`（比例是最后一次请求的输入 token 占窗口的百分比，见 `Renderer.contextShare`）。非 TTY 或 `NO_COLOR` 时退化为纯文本。
- 需要的事件：runner 现有 `tool_result`（仅 hooks 开启时发）改为 chat 始终订阅，并新增 `tool_call` 开始事件；stream-json 不输出这两类事件（契约不变）。
- Claude provider：读取 `base_url`（拼接规则见第 1 节）；`Stream` 用 Messages API `stream: true`，处理 `message_start`（输入与缓存 token）、`content_block_start`（tool_use 的 id/name）、`content_block_delta`（`text_delta` 实时输出、`input_json_delta` 累积参数）、`content_block_stop`（解析该块参数）、`message_delta`（stop_reason、输出 token）、`message_stop`、`ping`（忽略）与流中 `error`；只重试建连；Pool 已有"已输出则不切换备用"。

## 架构

```mermaid
flowchart TD
  main[cmd/suag main] --> cli[internal/cli 命令]
  cli --> output[internal/output]
  cli --> chat[internal/chat]
  cli --> rt[internal/wiring 装配]
  cli --> acp[internal/acpserver]
  chat --> hooks[internal/hooks]
  chat --> output
  chat --> personal
  rt --> cfg[internal/config]
  rt --> personal[internal/personal Runner]
  rt --> platform[internal/platform llm/tools]
  acp --> personal
```

## 关键流程

模型选择：`--model`/`SUAG_MODEL`/`model` → 解析为 `(provider, model)` → 合并预设与用户条目 → 按 `type` 构建 provider（携带 base_url、key、headers）→ 与 `fallback_models` 组成 Pool；`compact_model` 单独构建。

chat 一轮：输入 → 渲染器打印提示 → Runner 事件流（文本增量 / tool_call / tool_result / plan）→ 渲染 → 回合结束状态行 → hooks Stop（若配置）。

## 安全、数据、运维与回滚

- 数据：会话库与 stream-json/ACP 契约不变；旧会话可继续使用。
- 配置：旧 `llm` 段不再生效，升级后需按提示改写配置（Gofer 主机与本机各一份）；回滚到旧二进制即恢复旧配置读取。
- 安全：默认 MCP 放行变为空；只读模式只认 `tools.read_only_allow` 且必须带 server 限定，未配置时只读 job 中的 MCP 工具全部被拒（升级时需按迁移清单补配置）；hooks 默认关闭，只执行配置里的 argv（不经 shell）。密钥仍只来自配置/`.env`/环境变量，不进 argv。
- 回滚：每个阶段独立提交，可按阶段回退。

## 决策

- D1 YAML + `provider/model` 引用 + 内置三个预设（理由：沿用现有加载与 `.env` 机制，用户只学一种写法）。
- D2 先分层（纯移动、行为不变），再改配置，再做 chat 显示；Claude 流式与它们无依赖，可并行。
- D3 gofer 配合全部经配置和调用方参数表达，suag 内置默认对 Gofer 无特殊对待。

## 待确认事项

- Q1 旧 `llm` 段：直接报错并提示新写法（推荐，无历史包袱）／读取时自动转换为新结构。
- Q2 删除 `--provider` 与 `SUAG_PROVIDER`，只用 `--model provider/model`（推荐）／保留 `--provider` 作为选择 provider 的简写。
- Q3 Gofer 侧配合（job 环境设 `SUAG_HOOKS=off` 与 `SUAG_APPEND_SYSTEM_PROMPT`、经 run 投递 web 回复时加 `--injected`）与 suag 同批交付，由 gofer 会话在 Gofer 仓库实施（推荐）／suag 先保留兼容读取 `GOFER_JOB_ID` 一个版本。用户 suag 配置的迁移（`hooks`、`tools.read_only_allow`、`providers`）由 suag 文档与样例给出。

决议（2026-10-09）：Q1 旧 `llm` 段直接报错并提示新写法；Q2 删除 `--provider` 与 `SUAG_PROVIDER`；Q3 Gofer 侧配合同批交付，由 gofer 会话实施，suag 不保留 `GOFER_JOB_ID` 兼容。

## 结论与人工计划 Gate

确认 Q1–Q3 并批准本设计后，进入实施计划编写；计划经一轮合并评审与人工批准后才实施。
