<!-- template_id: plan; template_version: 1.2.0 -->
# Suag chat 写工具开关、交互确认与遗留小项 实施计划

> 状态：Draft 0.1 / 待人工计划批准

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-09 | Claude | 初稿 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

## 目标与完成定义

思考模式 RIGOROUS；核心目标：交付已批准设计 0.2 的三部分；scope freeze 即设计 0.2 的范围与非目标；review budget：一轮合并轴评审（低暴露度，SR1409）；停止条件：评审无 CORE_BLOCKING 并获人工计划批准。

完成定义：

1. `tools.allow_write: true` 时 `suag chat` 具备写工具，`suag run` 不受影响；`--read-only` 始终优先；ACP 不读此项。
2. chat（TTY）中调用 `file_write`/`shell_exec`/`web_search` 前显示完整关键参数并询问 `[y/N/a]`；`y` 执行、`a` 本进程后续同名工具放行、其他输入拒绝且回合继续、Ctrl-C/Ctrl-D 中止本回合并回到提示符；询问前清除预输入、保存并恢复未发送的半行输入、回答不进历史；`tools.approval: auto` 不询问；非 TTY 下 `ask` 一律拒绝并提示。
3. 回合结束提示按被拒工具名列出，不再出现 "Input required to authorize an MCP tool."。
4. 遗留小项：trace 显示计划；stream-json 新增 `tool_call`/`tool_result` 行；越界判断不依赖文案；toolDiff 不被特殊文件阻塞；`Runner.Run` 提前失败时 `SessionUsage` 非 0；工作记忆视图保留工具调用前文字。
5. 容器 race 全量、Windows 全量通过；真实二进制在伪终端下验证确认流程。

## 范围、排除项与授权

范围：suag 仓库 `internal/{config,wiring,chat,cli,output,acpserver,personal,platform/tools}`、`pkg/agentcore/memory`、README、`docs/usage/*`、`config/config.yaml`。

排除：MCP 放行规则、ACP 审批、`file_edit` 工具、Claude 思考内容、Windows 人工确认项、push 与发版、Gofer 仓库。

授权：设计 0.2 已批准（Q1 `ask`、Q2 非 TTY 拒绝、Q3 `allow_write` 只作用于 chat）；本计划需人工批准后实施；按功能点本地提交为默认授权。

- `host_or_non_offline_action=REQUIRED`

（Windows 主机测试经 gofer exec job；stream-json 新增行类型需通知 gofer 会话。）

## 输入与批准证据

- 设计：`docs/design/2026-10-09-chat-write-approval-design.md` 0.2（8f16f56 及批准标注）。设计评审一轮，1 条 CORE_BLOCKING（确认读取状态机）与 6 条 SHOULD、4 条 NIT 已在 0.2 吸收。
- 批准：用户 2026-10-09 “批准设计（Q1/Q2 按推荐）”“批准设计 0.2（Q3 按推荐）”。
- issue：`suag-iw3u` 第 2、3、4、5、8、9 项。

## Capability Discovery

### Capability decisions

| capability_id | required_capability | searched_candidates | direct_reuse | thin_adapter_or_owner_extension | decision | proven_gap | duplication_and_lifecycle_risk |
|---|---|---|---|---|---|---|---|
| CAP-01 | chat 写工具审批 | tools.Approval/Approver；wiring.cliMCPGate；acpserver 权限请求 | 部分：Approval 层与 Denied 语义直接复用 | 在 tools 定义 Asker 接口，wiring 增加包装 cliMCPGate 的 chat 审批器 | OWNER_EXTENSION | cliMCPGate 只判 MCP，不能询问 | 无，cliMCPGate 保留给 run |
| CAP-02 | 回合中读取一行回答 | chatInputSource 读请求/事件通道；lineedit | 部分：读请求与 lineedit 复用 | 在 chatInputSource 增加 Ask 状态机 | OWNER_EXTENSION | 现有读取只在回合之间工作，未处理 raw/cooked 与挂起读 | 无 |
| CAP-03 | 清除终端预输入 | golang.org/x/sys/unix（tcflush）、golang.org/x/sys/windows（FlushConsoleInputBuffer） | 是：已在依赖中 | 带 build tag 的小文件 | DIRECT_REUSE | none | 无 |
| CAP-04 | 遗留小项 | 各 owner 文件 | 是 | 在原位修改 | OWNER_EXTENSION | 各项即缺陷本身 | 无 |

### New module candidates

| candidate_id | capability_id | proposed_module | searched_candidates | direct_reuse_gap | thin_adapter_or_owner_extension_gap | proven_gap | unique_owner_and_lifecycle | deletion_or_merge_handling |
|---|---|---|---|---|---|---|---|---|
| NONE | none | none | none | none | none | none | none | none |

### Rejected new tools

| rejected_candidate | capability_id | deletion_test_and_reason |
|---|---|---|
| 通用终端交互库（如 cliui interact 的确认组件） | CAP-02 | 它独立读取 stdin，会与 chatInputSource 的读取器争抢输入；删除不影响验收 |

## 前置检查与 fail-closed 条件

- workspace baseline：Git 根 `inhere-tools/suag`，分支 `main`，HEAD 为本计划提交前的 `8f16f56` 之后的批准标注提交；`git status --short` 仅本计划文件；实施前再查，出现非本计划改动即 `Ownership Conflict` 停止。
- 依赖：不新增第三方依赖（x/sys 已在 go.mod）。
- 环境：容器内 race；Windows 经 gofer exec job；伪终端验证用容器内 `script -qfc`。
- fail-closed：stream-json 已有行的字段或顺序变化即停止（只允许新增行类型）；审批器绑定前任何需确认工具必须被拒绝，测试必须覆盖；任何测试触及真实 `~/.suag` 立即停止。
- 预期路径（规划证据）：`internal/platform/tools/{approval.go,asker.go}`、`internal/wiring/{cli_mcp.go,runtime.go,chat_approver.go}`、`internal/chat/{chat.go,chat_input.go,ask.go,flush_unix.go,flush_windows.go,render.go}`、`internal/cli/{run.go,chat.go,trace.go}`、`internal/output/streamjson.go`、`internal/cli/testdata/stream_json_contract.golden.json`、`internal/acpserver/{policy.go,open_unix.go,open_other.go}`、`internal/personal/{runner.go,trace.go}`、`internal/platform/tools/native/{pathresolve.go,file.go}`、`pkg/agentcore/memory/working.go`。

## 波次与依赖

| 波次 | 任务 | 依赖 | 并行 |
|---|---|---|---|
| W1 遗留小项 | T01 → T02 → T03 → T04 → T05 → T06 | 无 | 否（同一子 agent 顺序执行） |
| W2 写工具与确认 | T07 → T08 | W1 | 否 |
| W3 文档与回归 | T09 → T10 → T11 | W2 | 否 |

执行：W1 交 sonnet；T07–T08 涉及安全边界交 opus；本会话复核每个提交并运行验证。gofer plan 每任务一个 todo，按功能点本地提交。

## 任务

### T01 越界判断结构化

- 文件: `internal/platform/tools/native/{pathresolve.go,file.go}` 及测试。
- 动作: `isRootEscape` 改为 `errors.As(*os.PathError)` 且 `!errors.As(pe.Err, *syscall.Errno)`；`wrapRootErr` 同步；`realRelInRoot` 失败时返回其越界错误。
- 验证: `go test ./internal/platform/tools/native/`；新增用例：写入目录路径（EISDIR）不触发二次执行、越界路径仍拒绝、指向工作区内的绝对符号链接仍可用。
- 完成标准: 代码中不再匹配 "path escapes from parent"。
- 依赖: 无。

### T02 toolDiff 单次打开

- 文件: `internal/acpserver/policy.go`、新增 `open_unix.go`/`open_other.go`（build tag）及测试。
- 动作: 经 `os.Root` 打开一次（Unix 加 `O_NONBLOCK`），对句柄 `Stat`，非普通文件不预览，读取有上限；删除 Lstat + 二次打开。
- 验证: 测试：普通文件生成预览；目录与（Unix）FIFO 不预览且不阻塞（带超时断言）；越界仍不预览。
- 完成标准: 预览路径只打开一次。
- 依赖: T01。

### T03 提前失败时的会话用量

- 文件: `internal/personal/runner.go` 及测试；必要时 `internal/acpserver/usage.go`。
- 动作: `Runner.Run` 在 finish 之前返回错误的路径上，填充 `RunResult.SessionUsage`（复用 `SumUsage` 的现有调用方式）。
- 验证: 测试：已有用量的会话在规则读取或历史加载失败时，返回结果的 `SessionUsage` 等于先前累计值；ACP 测试断言此时 `usage_update` 的累计值非 0。
- 完成标准: suag-iw3u 第 5 项消除。
- 依赖: T02。

### T04 工作记忆视图

- 文件: `pkg/agentcore/memory/working.go` 及测试。
- 动作: 带工具调用的助手消息保留文字、去掉 `ToolCall`；纯工具调用（无文字）仍过滤。
- 验证: `go test ./pkg/agentcore/memory/`，断言文字保留、无孤立 ToolCall。
- 完成标准: suag-iw3u 第 2 项消除。
- 依赖: T03。

### T05 trace 显示计划

- 文件: `internal/cli/trace.go`、`internal/personal/trace.go`（或 store 读取处）及测试。
- 动作: 读取会话 plan 条目，按时间并入时间线输出 `plan ✔/▶/○ 步骤`；`--turn` 过滤；`--json` 输出 `{"kind":"plan",...}`。
- 验证: 测试覆盖文本、`--turn`、`--json` 三种输出。
- 完成标准: suag-iw3u 第 8 项消除。
- 依赖: T04。

### T06 stream-json 工具事件行

- 文件: `internal/output/streamjson.go`、`internal/cli/run.go`、`internal/cli/testdata/stream_json_contract.golden.json` 及测试。
- 动作: 新增 `tool_call`（`name`、`args` 摘要）与 `tool_result`（`name`、`is_error`、`summary`：先脱敏再截断）行；更新 golden（只增不改）。
- 验证: 契约测试通过；`git diff` 显示 golden 只有新增行；新增用例断言 summary 已脱敏。
- 完成标准: suag-iw3u 第 9 项消除。
- 依赖: T05。

### T07 `tools.allow_write`

- 文件: `internal/wiring/runtime.go`（OpenCLI）、`internal/cli/{run.go,chat.go}` 及测试。
- 动作: `wiring.Options` 增加 `ChatMode`；`OpenCLI` 中 `AllowWrite = (flag || (ChatMode && cfg)) && !ReadOnly`；run 不读配置；ACP 不变。
- 验证: 测试：chat 模式 + 配置 true 有 `shell_exec`；run 模式 + 配置 true 无 `shell_exec`；`--read-only` + 配置 true 无写工具；ACP 路径不受影响。
- 完成标准: 完成定义第 1 条。
- 依赖: T06。

### T08 chat 交互确认

- 文件: `internal/platform/tools/asker.go`（Asker 接口）、`internal/wiring/{chat_approver.go,runtime.go}`、`internal/chat/{chat.go,chat_input.go,ask.go,flush_unix.go,flush_windows.go,render.go}`、`internal/cli/chat.go` 及测试。
- 动作:
  - chat 审批器：MCP 走 cliMCPGate；只读名单放行；其余非 MCP 工具按 `tools.approval`（`ask`/`auto`，缺省 `ask`）处理；`a` 的放行集合按进程保存；Asker 未绑定或不可询问时返回 `tools.Denied`（含设置 `tools.approval: auto` 的提示）。
  - 后绑定：wiring 在 chat 模式下放入可绑定的 Asker 占位；cli 传给 `chat.Run`；chat 创建输入源后绑定，非 TTY 不绑定。
  - `chatInputSource.Ask`：按设计 0.2 的读取状态机实现（临时 raw、结束恢复 turnActive 与 cooked；询问前清除预输入；挂起读的半行保存/清空/恢复与原提示符恢复；Ack activity；CompleteLine；回答不入历史；Ctrl-C/Ctrl-D 返回中止错误）。
  - 询问提示显示完整关键参数（shell 命令、文件路径与字节数、搜索查询；脱敏不截断）；渲染器新增对应输出。
  - 中止：Asker 返回非 Denied 错误 → Approval 包装为 `ErrAborted` → 回合失败，chat 打印"已取消"并回到提示符。
  - 回合结束提示改为"本轮被拒绝的工具：…"。
- 验证: 单元测试覆盖 y/n/a/空行/Ctrl-C/Ctrl-D、挂起读半行恢复、预输入清除（以可注入的 flush 函数断言调用）、Ask 期间投递排队、未绑定拒绝、非 TTY 拒绝、`auto` 不询问、MCP 规则不变、3 次拒绝结束回合；`go test -race ./...`。
- 完成标准: 完成定义第 2、3 条。
- 依赖: T07。

### T09 文档与样例

- 文件: `README.md`、`docs/usage/gofer-personal-agent.md`、`docs/usage/llm-config-v0.md`（如涉及）、`config/config.yaml`。
- 动作: 说明 `tools.allow_write`（只作用于 chat）、`tools.approval`、确认交互与 Ctrl-C 语义、需确认工具范围、stream-json 新行类型（未配置 `ndjson_keep` 的调用方会收到）；删除 README 中 "the CLI has no mid-turn approval prompt" 的过时说法。
- 验证: `grep -n "no mid-turn approval" README.md` 无结果；样例配置以 `SUAG_CONFIG` 加载实跑成功。
- 完成标准: 文档与行为一致。
- 依赖: T08。

### T10 回归与伪终端验证

- 文件: 无新增。
- 动作: 容器 `gofmt -l .`、`go vet ./...`、`go test -race ./...`；Windows 全量 job；真实二进制 + 假模型服务（返回 `shell_exec` 调用）在 `script -qfc` 伪终端下跑 chat：分别输入 `y`、`n`、`a`、Ctrl-C，检查执行与否、提示与回合结束行为；非 TTY 管道输入验证自动拒绝。
- 验证: 命令输出与 job id 记录到 progress。
- 完成标准: 全部符合完成定义。
- 依赖: T09。

### T11 收尾

- 文件: `.gofer/tracker`。
- 动作: 在 `suag-iw3u` 记录第 2、3、4、5、8、9 项已修；向 gofer 会话通知 stream-json 新增行类型；更新 gofer plan 状态。
- 验证: `gofer repo status --changed` 只含预期变化。
- 完成标准: 通知送达，tracker 已提交。
- 依赖: T10。

## 回滚与恢复

- 每个任务独立提交，可单独 `git revert`。T08 回退后 chat 恢复为现行为（不询问），T07 回退后配置项失效。
- 恢复点：每个波次结束的提交；中断时按 gofer plan todo 与最近提交续做。

## 人工 Gate

- 本计划批准后才开始实施。
- push 与主机安装不在本计划授权内，完成后单独请求。

## 可追溯性

| 设计要求 | 任务 | 验证 |
|---|---|---|
| `tools.allow_write` 只作用于 chat、只读优先（Q3） | T07 | 模式组合测试 |
| 交互确认、`ask` 缺省（Q1）、非 TTY 拒绝（Q2）、读取状态机（评审 CORE_BLOCKING）、完整参数、Ctrl-C 中止本回合（D5）、只读名单之外一律询问（D4） | T08 | 单元测试 + T10 伪终端验证 |
| 拒绝计数与提示 | T08 | 3 次拒绝测试、提示文本断言 |
| 越界判断结构化 | T01 | EISDIR 与越界用例 |
| toolDiff 单次打开 | T02 | FIFO/目录用例 |
| ACP 用量 | T03 | 提前失败用例 |
| 工作记忆视图 | T04 | 视图用例 |
| trace 计划 | T05 | 三种输出用例 |
| stream-json 工具事件 | T06 | golden 只增、脱敏断言 |
| 文档 | T09 | grep 与样例实跑 |

## 完成 Gate 与剩余工作

完成条件：T01–T11 全部完成且完成定义 1–5 均有证据。剩余：Windows 人工确认项、Claude 思考内容、`file_edit` 工具、push 与主机安装。
