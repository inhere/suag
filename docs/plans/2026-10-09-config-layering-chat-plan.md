<!-- template_id: plan; template_version: 1.2.0 -->
# Suag 配置重设计、分层整理与 chat 显示 实施计划

> 状态：Draft 0.1 / 待人工计划批准

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-09 | Claude | 初稿 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

## 目标与完成定义

思考模式 RIGOROUS；核心目标：按已批准设计 0.2 交付四项结果；scope freeze 即设计 0.2 的范围与非目标；review budget：一轮合并轴评审（低暴露度，SR1409）；停止条件：评审无 CORE_BLOCKING 并获人工计划批准。

完成定义（每条都有可执行验证）：

1. 分层：`internal/app` 不存在；`cmd/suag` 只有 `main.go`（装配与 exitReport）；命令定义在 `internal/cli`；stream-json golden 契约测试与全量 `go test -race ./...` 通过且契约 golden 文件未改。
2. 配置：`providers` + `model: provider/model` 生效；`--model` 接受同样写法；内置 openai/anthropic/deepseek 预设；`fallback_models`、`compact_model`、按模型的 `context_window` 生效；旧 `llm` 段与 `--provider`/`SUAG_PROVIDER` 启动即报错并提示新写法。
3. 中立：`grep -rni gofer --include=*.go internal cmd`（不含测试与样例注释）无结果；只读模式只认 `tools.read_only_allow`；hooks 只在配置 `hooks.command` 时启用；`suag send --source`、`run --injected` 可用。
4. chat 显示：TTY 下模型文本实时输出、工具调用与结果各一行、计划勾选列表、错误着色、每轮一行状态；非 TTY 或 `NO_COLOR` 时纯文本；stream-json 不变。
5. Claude：`type: anthropic` 读 base_url 并真正流式（文本实时、tool_use 参数拼接、用量）。
6. 文档、配置样例与 Gofer 迁移清单同步；Windows 主机全量测试通过。

## 范围、排除项与授权

范围：suag 仓库 `cmd/`、`internal/`、`config/config.yaml`、`README.md`、`docs/usage/*`、`AGENTS.md` 入口说明；新增依赖 `github.com/gookit/cliui v0.5.3`（设计 0.2 与用户要求指定）。

排除：会话存储与迁移、stream-json/ACP 字段、`pkg/agentcore` 公共接口、Gofer 仓库（迁移清单交 gofer 会话实施）、`suag-iw3u` 遗留项、push/发版。

授权：设计 0.2 已批准（Q1–Q3 按推荐）；本计划需人工批准后才实施。按功能点本地提交为默认授权；push 另行授权。

- `host_or_non_offline_action=REQUIRED`

（Windows 主机测试经 gofer exec job；向 gofer 会话发送迁移清单。）

## 输入与批准证据

- 设计：`docs/design/2026-10-09-config-layering-chat-design.md` 0.2（4624e4c 及批准标注提交）。设计评审：一轮合并轴评审，1 条 CORE_BLOCKING（只读模式放行）与 9 条 SHOULD 已在 0.2 吸收。
- 批准：2026-10-09 用户原话“可以 按推荐来”，对应 Q1 旧 `llm` 段报错、Q2 删除 `--provider`、Q3 Gofer 侧配合同批交付。
- issue：suag-kai3、suag-2zbi、suag-c7co、suag-l5i7（剩余 Claude 部分）。

## Capability Discovery

### Capability decisions

| capability_id | required_capability | searched_candidates | direct_reuse | thin_adapter_or_owner_extension | decision | proven_gap | duplication_and_lifecycle_risk |
|---|---|---|---|---|---|---|---|
| CAP-01 | 分层后的包结构 | internal/app（现有全部职责）；cmd/suag | 否：app 同时承担命令、装配、输出、chat、hooks | 按职责拆分移动到新包，不新增逻辑 | MINIMAL_NEW_MODULE | app 单包无法表达依赖方向，也是 suag-2zbi 的问题本身 | internal/app 在 T05 删除，不并存 |
| CAP-02 | 多 provider 配置模型与模型引用解析 | internal/config/llm.go（单 llm 段 + provider 覆盖）；context_window.go | 否：单段结构无法表达多个 provider | 在 internal/config 内替换读取模型 | OWNER_EXTENSION | 现有结构就是要被替换的对象 | 旧读取函数在 T07 删除 |
| CAP-03 | Claude 流式与 base_url | internal/platform/llm/claude.go；openai_stream.go 的实现方式 | 部分：可沿用 OpenAI 流式的重试与 Pool 规则 | 在 claude.go 旁新增 claude_stream.go | OWNER_EXTENSION | Anthropic SSE 事件格式不同于 OpenAI | 无 |
| CAP-04 | 通用 hooks | internal/app/gofer_hooks.go | 部分：协议与等待语义保留 | 移到 internal/hooks 并改为读 `hooks.*` 配置 | OWNER_EXTENSION | 写死 gofer 命令与 auto 模式 | gofer.* 配置在 T08 删除 |
| CAP-05 | 终端渲染 | gookit/cliui（show）；goutil 颜色；现有 liveText | 是：cliui/颜色组件 | 在 internal/chat 写渲染器组合使用 | DIRECT_REUSE | none | 不自造颜色/框线工具 |
| CAP-06 | 工具调用事件 | personal Runner 的 tool_result 事件；agentcore ToolEvent hooks | 部分：tool_result 已有 | 在 runner 现有 hook 中补发 tool_call 开始事件 | OWNER_EXTENSION | chat 需要在工具执行前显示调用 | 无 |

### New module candidates

| candidate_id | capability_id | proposed_module | searched_candidates | direct_reuse_gap | thin_adapter_or_owner_extension_gap | proven_gap | unique_owner_and_lifecycle | deletion_or_merge_handling |
|---|---|---|---|---|---|---|---|---|
| NEW-01 | CAP-01 | internal/cli、internal/wiring、internal/output、internal/chat、internal/hooks | internal/app；cmd/suag | app 单包混合职责，无法复用为分层结构 | 留在 app 内拆文件不能形成依赖方向，也不解决 cmd 入口里的命令定义 | suag-2zbi 要求入口只装配、命令与逻辑分离 | 各包只承接从 app/cmd 移出的代码，随 suag 主程序生命周期 | internal/app 与 cmd/suag/observe_cmd.go 在 W1 删除，代码只存在一份 |

### Rejected new tools

| rejected_candidate | capability_id | deletion_test_and_reason |
|---|---|---|
| 自写 SSE 通用解析库 | CAP-03 | 两个 provider 的事件结构不同，各自 bufio 扫描约 100 行即可；删掉不影响验收 |
| 配置迁移命令 `suag config migrate` | CAP-02 | Q1 决定不自动迁移；删掉不影响验收 |

## 前置检查与 fail-closed 条件

- workspace baseline（2026-10-09 规划时）：Git 根 `inhere-tools/suag`，分支 `main`，HEAD `4624e4c`，dirty 仅本计划与设计批准标注（本计划提交时一并提交）；无需保护的 untracked 文件。实施前再执行 `git status --short`，出现非本计划产生的 dirty 文件即 `Ownership Conflict`，停止。
- 依赖：Go 1.25；新增 `github.com/gookit/cliui v0.5.3`（T12 引入）；不新增其他第三方依赖。
- 环境：容器内 Go 跑 race；Windows 主机经 `gofer job run -p my-tools-dev -a exec --runner local --cwd inhere-tools/suag --sync -- go test ./...`。
- 测试隔离：各测试包 `TestMain` 继续把 `SUAG_HOME` 设为临时目录；hooks 关闭开关在 T08 由 `SUAG_GOFER_HOOKS=off` 改为 `SUAG_HOOKS=off`；任何测试触及真实 `~/.suag` 立即停止。
- 预期路径（规划证据，非封闭白名单）：新增 `internal/{cli,wiring,output,chat,hooks}/`、`internal/platform/llm/claude_stream.go`、`internal/config/providers.go`；删除 `internal/app/`、`cmd/suag/observe_cmd.go`；修改 `cmd/suag/main.go`、`internal/config/*`、`internal/platform/llm/{claude.go,pool.go}`、`internal/personal/{runner.go,observe.go}`、`internal/acpserver/server.go`（注释）、文档与样例。
- fail-closed：W1 任一步 golden 契约或全量测试结果变化即停止（W1 不允许行为变化）；需要改 `pkg/agentcore` 公共接口时停止；发现设计未覆盖的行为取舍按 `Semantic Amendment` 返回。

## 波次与依赖

| 波次 | 任务 | 依赖 | 并行 |
|---|---|---|---|
| W1 分层（行为不变） | T01 → T02 → T03 → T04 → T05 | 顺序 | 否 |
| W2 配置与中立 | T06、T10 并行 → T07 → T08 → T09 | T05 | T06 与 T10 文件不重叠 |
| W3 chat 显示 | T11 → T12 | T08 | 否 |
| W4 回归与收尾 | T13 → T14 | T12 | 否 |

执行方式：机械移动（W1）交 sonnet 子 agent；配置模型、Claude 流式、中立化与渲染交 sonnet 并由本会话复核，涉及安全边界的 T08 交 opus（SR1432）。每个任务一个 gofer plan todo，按功能点本地提交；子 agent 禁止 `git stash`/`reset`，只提交自己的文件，行为改动须给出实际运行的验证命令。

## 任务

### T01 抽出 internal/output

- 文件: 新增 `internal/output/`（`livetext.go`、`streamjson.go`、`usage.go`）；修改 `internal/app/{cli.go,chat.go}` 改为调用 output。
- 动作: 移动 `liveText`、`withUsage` 与 stream-json 行编码（session/turn_start/message_delta/plan/turn_end/result/input_required/error 行的构造）为 output 的导出函数；app 仅调用。
- 验证: `go test -race ./...`；`git diff --stat internal/app/testdata` 为空。
- 完成标准: app 中不再有 stream-json 行字面量构造；golden 测试通过。
- 依赖: 无。

### T02 抽出 internal/wiring

- 文件: 新增 `internal/wiring/`；移动 `internal/app/{capabilities.go,cli_mcp.go,app.go}` 与 `cli.go` 中的 `openPersonalRuntime*`、`contextWindowForSettings`、`appendSystemPrompt`、`buildPersonal*Capabilities`；对应测试 `capabilities_test.go`、`cli_mcp_startup_test.go`、`tool_visibility_test.go` 随迁。
- 动作: 导出 `wiring.Open(ctx, Options) (*Runtime, error)` 与 `Runtime.Close`；`Options` 承接现有 personalOptions 中装配所需字段；MCP provider 工厂测试替身用 `export_test.go` 暴露。
- 验证: `go test -race ./...`；`go vet ./...`。
- 完成标准: app 只通过 `wiring.Open` 获得 Runner；随迁测试全部在 wiring 包通过。
- 依赖: T01。

### T03 抽出 internal/hooks

- 文件: 新增 `internal/hooks/`（来自 `internal/app/gofer_hooks.go`）；修改 `internal/app/chat.go`。
- 动作: 原样移动 hooks 客户端、事件/响应类型与 Stop 等待；导出 chat 所需接口（Client、Wait、Event、Response）；行为与配置键暂不变。
- 验证: `go test -race ./...`（chat_hooks_test 仍在 app，经导出类型编译通过）。
- 完成标准: app 中无 hooks 进程调用代码。
- 依赖: T02。

### T04 抽出 internal/chat

- 文件: 新增 `internal/chat/`（来自 `internal/app/{chat.go,chat_input.go}`）；测试 `chat_input_test.go`、`chat_hooks_test.go` 随迁（后者按主要被测对象归 chat）。
- 动作: 导出 `chat.Run(ctx, runner, Options, stdin, stdout, stderr, hooks.Client)`；chat 依赖 output、hooks、personal，不依赖 wiring。
- 验证: `go test -race ./internal/chat/ ./...`。
- 完成标准: chat 相关测试在 chat 包通过，数量与迁移前一致（迁移前后 `go test -list . ` 计数对比记录到 progress）。
- 依赖: T03。

### T05 建立 internal/cli，删除 internal/app

- 文件: 新增 `internal/cli/`（`root.go`、`run.go`、`chat.go`、`acp.go`、`ps.go`、`send.go`、`trace.go`、`stats.go`、`export.go`、`version.go`、`errors.go`）；移动 `internal/app/cli.go` 剩余部分、`acp.go`、`cmd/suag/observe_cmd.go` 与 `cmd/suag/main.go` 中的命令定义；测试 `cli_test.go`、`cli_mcp_test.go`、`acp_test.go`、`stream_json_*_test.go`、`usage_output_test.go`、`main_test.go`（命令部分）、`observe_cmd_test.go` 随迁；删除 `internal/app/`。
- 动作: `cmd/suag/main.go` 只保留 main、`exitReport`、版本注入；ACP 会话工厂闭包放在 `internal/cli/acp.go`（cli 依赖 acpserver 与 wiring，acpserver 不 import wiring）；`SessionNotFoundError`、`processExitError` 归 `internal/cli/errors.go`。
- 验证: `go test -race ./...`；`go vet ./...`；`go list -deps ./internal/acpserver | grep internal/wiring` 为空；`test ! -d internal/app`。
- 完成标准: 完成定义第 1 条成立；W1 前后全量测试名单一致。
- 依赖: T04。

### T06 新配置模型

- 文件: `internal/config/{providers.go,llm.go,context_window.go}` 及测试。
- 动作: 定义 `ProviderConfig{Type,BaseURL,APIKey,Headers,Models}`、内置预设（openai/anthropic/deepseek 的 base_url 与 key 环境变量）、`ResolveModel(ref) (ModelSpec, error)`（按第一个 `/` 且前缀为已定义 provider 才切分）、`ReadModelSettings(flagModel)`（flag > `SUAG_MODEL` > `model`）、`compact_model`/`fallback_models` 解析、`ContextWindow(spec)` 单一来源；检测到 `llm` 段、`SUAG_PROVIDER` 时返回带新写法提示的错误。
- 验证: `go test ./internal/config/`，覆盖：预设覆盖合并、裸 model、带 `/` 的 model id、未定义 provider 前缀、fallback/compact 裸 model、context_window 优先级、旧段报错文案。
- 完成标准: 新函数全部有表驱动测试；旧读取函数暂留供 T07 切换。
- 依赖: T05。

### T10 Claude provider：base_url 与 SSE 流式

- 文件: `internal/platform/llm/{claude.go,claude_stream.go,claude_stream_test.go}`。
- 动作: `ClaudeConfig.BaseURL`（`/v1` 结尾则拼 `/messages`，否则拼 `/v1/messages`，缺省 `https://api.anthropic.com`）；`Stream` 发送 `stream: true`，处理 message_start（输入/缓存 token）、content_block_start（tool_use id/name）、content_block_delta（text_delta 实时 emit、input_json_delta 累积）、content_block_stop、message_delta（stop_reason、输出 token）、message_stop、ping、error；只重试建连；非 event-stream 响应按普通响应解析。
- 验证: `go test ./internal/platform/llm/`，用 httptest 覆盖文本流、tool_use 分片、usage、流中 error、4xx、base_url 两种写法。
- 完成标准: Claude `Stream` 不再调用 `Generate`。
- 依赖: T05（与 T06 并行）。

### T07 装配切换到新配置

- 文件: `internal/wiring/*`、`internal/config/llm.go`（删旧函数）、`internal/cli/{run.go,chat.go}`（删除 `--provider`）、`internal/acpserver/server.go`（注释）及相关测试。
- 动作: wiring 按 `ModelSpec` 构建主 provider（含 headers、base_url）、`fallback_models` 组成 Pool、`compact_model` 单独构建；会话元数据记录 provider id 与主模型；删除 `ReadLLMProviderSettingsWithOverrides` 等旧路径与 `--provider` flag。
- 验证: `go test -race ./...`；新增测试：`--model deepseek/deepseek-chat` 指向预设、fallback 在主 provider 失败时生效、旧 `llm` 段启动报错。
- 完成标准: 完成定义第 2、5 条（装配侧）成立；代码中无 `SUAG_PROVIDER`、`llm.api_type` 读取。
- 依赖: T06、T10。

### T08 gofer 行为改为通用配置

- 文件: `internal/wiring/` 中 MCP gate、`internal/hooks/*`、`internal/chat/*`、`internal/personal/runner.go`、`internal/cli/{run.go,send.go}`、各 `TestMain` 与 hooks 相关测试。
- 动作: 内置放行清空，新增 `tools.read_only_allow`（只读模式唯一名单，必须 `server/toolglob`）；hooks 读 `hooks.command`/`timeout_ms`/`stop_args`/`post_tool_use`，未配置即关闭，`SUAG_HOOKS=off` 关闭，删除 auto 模式与 `GOFER_JOB_ID` 判断；删除 GOFER_JOB_ID 提示词；投递前缀改为通用规则（`send --source`，文本已带 `[…] ` 标签不再加）；`run --injected` 替代前缀识别；测试开关改名 `SUAG_HOOKS`。
- 验证: `go test -race ./...`；新增测试：只读模式 `read_only_allow` 放行与拒绝、未配置 hooks 不调用命令、`stop_args` 只追加到 Stop、`post_tool_use` 过滤与改名、`--source` 前缀、`--injected` 写入 transcript；`grep -rni gofer --include=*.go internal cmd | grep -v _test.go` 无结果。
- 完成标准: 完成定义第 3 条成立。
- 依赖: T07。

### T09 文档、样例与 Gofer 迁移清单

- 文件: `config/config.yaml`、`README.md`、`docs/usage/{llm-config-v0.md,gofer-personal-agent.md}`、`AGENTS.md`、`docs/README.md`。
- 动作: 按新配置格式重写样例（含 OpenCode Go、anthropic 网关、read_only_allow、hooks 示例）；`llm-config-v0.md` 改为新格式说明；Gofer 使用文档加“升级迁移”一节：用户配置改写、`hooks.command`、`tools.read_only_allow`，以及 Gofer 侧 `SUAG_HOOKS=off`、`SUAG_APPEND_SYSTEM_PROMPT`、`--injected`。
- 验证: 用 T05 后的二进制以样例配置 `SUAG_CONFIG=config/config.yaml suag version`/`run --help` 加载成功；`grep -rn "llm:\|--provider\|SUAG_PROVIDER\|gofer.hooks" README.md docs/usage config` 无残留。
- 完成标准: 文档描述与 T07/T08 行为一致。
- 依赖: T08。

### T11 工具调用事件

- 文件: `internal/personal/{runner.go,observe.go}` 及测试、`internal/chat/*`。
- 动作: Runner 在工具执行前发 `tool_call` 事件（名称、参数摘要），`tool_result` 不再依赖 `EmitToolResults`（chat 始终订阅；stream-json 忽略这两类）。
- 验证: `go test -race ./...`；golden 契约文件不变；新增测试断言事件顺序 tool_call → tool_result。
- 完成标准: chat 能在执行前收到调用事件。
- 依赖: T08。

### T12 chat 渲染（cliui）

- 文件: `internal/chat/render.go`、`render_test.go`、`go.mod/go.sum`（cliui v0.5.3）。
- 动作: 渲染器：提示符着色；文本实时输出（复用 output.LiveText）；工具调用一行（名称 + 截断参数）、结果一行（成功/失败 + 截断摘要）；`update_plan` 勾选列表；错误红色；每轮结束灰色状态行（模型 · in/out tokens · 耗时）；非 TTY 或 `NO_COLOR` 退化纯文本。
- 验证: `go test ./internal/chat/` 用缓冲区断言纯文本模式输出；真实二进制以 OpenAI 与 Anthropic SSE 假服务跑一轮含工具调用的 chat（stdin 管道，纯文本模式），检查输出顺序与状态行。
- 完成标准: 完成定义第 4 条成立。
- 依赖: T11。

### T13 全量回归

- 文件: 无新增（修复按发现分类处理）。
- 动作: 容器 `gofmt -l .`、`go vet ./...`、`go test -race ./...`；Windows 主机全量测试 job；真实二进制冒烟：新配置（openai + anthropic 假服务）run/chat/send/trace/stats、旧 `llm` 段报错、只读模式 MCP 放行。
- 验证: 上述命令输出与 job id 记录到 progress。
- 完成标准: 全部通过。
- 依赖: T12。

### T14 收尾

- 文件: `.gofer/tracker`。
- 动作: 关闭 suag-kai3、suag-2zbi、suag-c7co、suag-l5i7；向 gofer 会话发送迁移清单；更新 gofer plan 状态。
- 验证: `gofer repo status --changed` 只含预期 issue 变化。
- 完成标准: issue 关闭、迁移清单已送达。
- 依赖: T13。

## 回滚与恢复

- 每个任务独立提交；W1 任一步失败可 `git revert` 该提交回到上一步（W1 不改行为）。
- 配置格式切换（T07）后若需回退，回滚 T06–T09 提交即恢复旧 `llm` 段读取；用户配置需同步回退。
- 恢复点：每个波次结束的提交；中断时按 gofer plan todo 与最近提交续做。

## 人工 Gate

- 本计划批准后才开始实施。
- push 与 Gofer 主机二进制升级不在本计划授权内，完成后单独请求。

## 可追溯性

| 设计要求 | 任务 | 验证 |
|---|---|---|
| 分层与依赖方向、ACP 工厂在 cli、测试迁移原则 | T01–T05 | golden + 全量 race、`go list -deps` 检查、`internal/app` 不存在 |
| 多 provider、模型引用、预设、fallback/compact、context_window、旧配置报错（Q1/Q2） | T06、T07 | config 表驱动测试、装配测试 |
| Claude base_url 与 SSE | T10 | httptest 流式测试 |
| 只读放行 `tools.read_only_allow`（设计评审 CORE_BLOCKING） | T08 | 只读放行/拒绝测试 |
| hooks 通用化、来源前缀、`--injected`、删除 GOFER_JOB_ID（Q3） | T08、T09 | hooks 测试、grep 检查、迁移清单 |
| chat 显示 | T11、T12 | 事件顺序测试、渲染测试、真实二进制冒烟 |
| 文档同步 | T09 | 样例加载、grep 无残留 |

## 完成 Gate 与剩余工作

完成条件：T01–T14 全部完成且完成定义 1–6 均有证据。剩余工作（不在本批）：`suag-iw3u` 遗留项、Claude extended thinking 回传、push 与主机升级。
