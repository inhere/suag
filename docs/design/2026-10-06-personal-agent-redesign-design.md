<!-- template_id: design; template_version: 1.1.1 -->
# Suag 个人 Agent 收敛设计

> 状态：Draft 0.4 / 数据目录选择已确认，实施待修订计划 Gate
>
> 存储部分已被 [2026-10-08 核心能力演进设计](2026-10-08-core-capabilities-design.md) 取代。

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-06 | Codex | 从 Agent OS 收敛为个人 Agent，确定 Gofer 首要接入方向与迁移边界 |
| 0.2 | 2026-10-06 | Codex | 加入 Gofer ACP 常驻交互，明确删除旧 Web 与隔离 legacy 代码 |
| 0.3 | 2026-10-06 | Codex | ACP 审批优先限定在 Suag Agent，明确先删除 Web 引用再移动旧 bridge |
| 0.4 | 2026-10-07 | Codex | 个人 CLI/ACP 默认会话数据放入 `~/.suag/db`，明确旧数据与只读边界 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

## 背景与目标

**核心目标**：用户能在自己的项目中通过终端或 Gofer 启动同一个 Suag Agent，让它完成任务、实时报告进度、保存会话，并在同进程或新进程中继续对话。Gofer 可以通过 ACP 与它常驻交互，也可以用 CLI 执行一次性任务。Suag 应易于增加模型、工具和技能，不再维护自己的 Web 控制面。

本设计采用 `RIGOROUS`；范围冻结为 Suag 的单 Agent 主链、CLI 与 ACP/Gofer 合同、旧 Web 删除和 legacy 隔离，以及数据与权限边界。其他平台协议、部署和数据库方案先延后。审核预算为一轮事实核对和一轮设计确认；若协议、会话恢复或安全边界未定，停在设计 Gate。

Gofer 已负责项目与执行节点选择、异步作业、日志、锁、重试、审批和 Web 控制面。它的 Steward 已负责整理工作项；Suag 定位为实际执行用户任务的 Agent，不复制工作项控制面。Suag 当前又包含 Host/Web、旧 orchestrator/team、cron、ACP bridge 和 agentcore，继续扩展为另一套控制面会使个人 Agent 的核心路径难以辨认。

## 名词

| 名词 | 本设计中的含义 |
|---|---|
| Suag session | 一段可在进程退出后继续的个人对话，外部只需持有 `session_id` |
| turn | 用户的一次输入及其模型/工具循环；内部可包含多个 run step |
| agentcore | 现有 `pkg/agentcore`，负责模型与工具循环、状态和 transcript |
| adapter | 把同一 Agent 能力接到 CLI 或 ACP stdio 的接入实现 |
| ACP Agent | Suag 在 stdio 上提供的 Agent Client Protocol 服务端，由 Gofer 作为客户端启动 |
| Gofer job | Gofer 调度的一次外部进程执行；可与 Suag session 跨多次 job 对应 |

## 范围与非目标

本次目标是 **单用户、单 Agent、单会话内的可靠执行**。首个可验收切片包含无服务 CLI 执行、ACP stdio 多轮交互与新进程续聊、流式事件、只读与工具审批、可替换模型/工具，以及 Gofer `cli-agent` / `acp-agent` 两条真实作业验证。交互式 `suag chat` 使用同一执行主链。

本次不建设多 Agent 团队、任务 DAG、另一套作业队列/远端 worker、IM 渠道、插件市场或自动学习系统。不以 Pigo 为运行时依赖；仅学习其小循环、headless/REPL、结构化事件与可嵌入接口。旧 Web 前后端在新 CLI/ACP 达到验收后删除；旧 team、cron、orchestrator 等不再属于运行主链，迁到只供参考的 `legacy/`。

受影响的代码归属仅为 `suag` Git 仓库；Gofer 侧首阶段只提供配置与联调样例，不修改其源代码。后续若形成共享协议或 Gofer 源码改动，重新判定文档归属与实施范围。

## 已确认事实与规范

- 当前 CLI 的 `chat` 调用旧 `orchestrator.RunTask`；当前 Host 的默认内部 runner 通过 `internal/agent` 适配到 `pkg/agentcore`。两个入口尚未共享一条简明的个人 Agent 接口。证据：`cmd/suag/main.go`、`internal/app/chat.go`、`internal/host/intrunner/runner.go`、`internal/agent/agentcore_runner.go`。
- Host 已有 SQLite session/turn/run、Web 事件、MCP tool source、技能解析和 recap；`pkg/agentcore` 已有 run/resume、tool loop、transcript。现有 Go 测试可通过，但 `intrunner` 的 session history 和 suspended runner 持有于进程内 map，跨进程续跑仍需专项验证与修复。证据：`internal/host/session`、`internal/app/capabilities.go`、`internal/host/intrunner/runner.go`。
- Gofer 当前 `cli-agent` 支持 argv 模板、`session_inject`、`session_resume`、`read_only_args` 和 NDJSON 通用投影；通用投影能从 `session` 行读取 id，并通过 `ndjson_stdout_path` 抽取最终答复。证据：Gofer 源码 HEAD `2ef84014` 的 `config/gofer.example.yaml`、`internal/config/model.go`、`internal/runner/ndjsonfilter/projector.go`；本机 CLI 报告版本 `0.81.0-1-g11aef54`，两者需在联调时重新核对。
- Gofer 的 `acp-agent` 通过 stdio 调用 `initialize`、`session/new|load|prompt|cancel|set_mode`，接收 `session/update`，并可处理 Agent 发起的 `session/request_permission`；常驻 job 以 `--session` 和 `job say/end` 连续交互。证据：Gofer `internal/runner/acp/runner.go`、`docs/design/2026-09-17-acp-agent-and-approval-gate-design.md`、当前 CLI `job run --help`。Suag 现有 `internal/host/acpbridge` 是 ACP **客户端**，不能当作该服务端复用。
- `github.com/coder/acp-go-sdk` 提供 Agent 侧 stdio 连接和 `AgentLoader`，其当前 `go.mod` 要求 Go 1.21，可作为实现候选；须先做与本机 Gofer 的协议兼容试验。参考 [ACP Go SDK](https://github.com/coder/acp-go-sdk/tree/0845a3bb9eddda5bfc22a94dd3598c90cb842451)。
- Pi 提供交互、print/JSON、RPC/SDK 与扩展入口；Pigo 公开 `agent.Session`、`Prompt/Stream/StreamEvents` 和自定义工具，并提供 headless/REPL、session resume。参考 [Pi coding agent](https://github.com/earendil-works/pi/tree/8b5708dbb1b4a819f924b43a2de02c9cc8c7a46d/packages/coding-agent) 与 [Pigo](https://github.com/smallnest/pigo/tree/891d1f372cefa92b5f5a104db20521238ba5a9fe)。Pigo 当前要求 Go 1.27+，Suag 是 Go 1.25，故不直接引入。
- 此变更涉及对外 CLI 合同、会话状态和权限语义，按 IDEV-STD 的 SR1107、SR1137、SR1408 走 Design → Planning → Implement；本草案不构成实施授权。
- 当前 `ResolveSessionDBPath` 与 `ResolveHostAgentcoreRunRoot` 默认使用相对路径 `db/`，个人 CLI checkpoint 也调用这两个函数。用户于 2026-10-07 明确指定新默认位置为 `~/.suag/db`；已有仓库 `db/` 内容仍按用户数据保留。

## 总体方案

**保留一个执行内核、一个会话真源、两个薄接入。** `pkg/agentcore` 继续负责模型/工具循环；在它与用户入口之间收敛一个个人会话模块，负责打开会话、装配 prompt/能力、运行 turn、保存结果和发事件。CLI 和 ACP stdio 使用同一个模块，不各自实现 Agent 循环。

第一阶段复用现有 `internal/host/session` 的持久化与 `pkg/agentcore` 存储，不立刻更换数据库或复制一份 session schema。将当前 `intrunner -> internal/agent -> agentcore` 的旧适配链逐步收短为 session 模块直接驱动 agentcore；先建立 CLI/ACP 共同执行路径，再替换旧链。外部只暴露 `session_id`；内部 `turn_id/run_id` 留给事件和诊断。旧 Web gateway 的测试仍引用出站 ACP bridge，因此清理顺序为新路径验收、删除 Web 引用、再移动旧 bridge 与控制面。

个人 CLI 与 ACP 共用的默认数据根为用户主目录下的 `~/.suag/db`：会话 SQLite 位于 `~/.suag/db/suag.db`，agentcore run/transcript 位于 `~/.suag/db/agentcore/host`。通过用户主目录 API 解析 `~`，不能把波浪号当成字面路径。显式 `database_url` 和 `agentcore.run_root` 继续覆盖各自默认值；旧 Web 在删除前保持原有默认目录。既有仓库 `db/` 不自动复制或迁移，需读取旧数据时可显式配置其路径。

扩展入口先只保留现有已验证的变化点：Provider、Tool registry/MCP source、分层 prompt/skills。插件 ABI、动态加载与自动生成技能需要第二个真实用例后再定。公开 Go 嵌入需求先由现有 `pkg/agentcore` 承担；只有出现第二个非 CLI 调用者时，才为“个人会话”新增公开 Go facade。

| 现有资产 | 处理方向 |
|---|---|
| `pkg/agentcore`、Provider、tool registry/MCP、skills | 保留并接入单 Agent 主链 |
| `internal/host/session`、SQLite transcript | 收缩成会话与执行状态的内部实现，保留已有数据 |
| `cmd/suag`、`internal/app/chat.go` | 改为同一单 Agent 的 headless/REPL/ACP stdio 入口 |
| `internal/agent` 适配层、旧 `orchestrator/team`、cron、出站 `acpbridge` | 新主链脱离其导入后移至隔离的 `legacy/`，仅供参考 |
| `web/app`、Host 的 Web gateway/AG-UI 与 `server` 命令 | 新 CLI/ACP 验收后删除，相关测试与文档同时收口 |
| 自动 memory candidate 管线 | 暂不进入默认个人 Agent 路径，保留代码待真实使用验证 |

## 架构

```text
终端 suag chat/run ──> CLI adapter ─┐
Gofer acp-agent ─────> ACP adapter ─┼─> 个人会话模块 ─> pkg/agentcore ─> Provider
Gofer cli-agent ─────> CLI adapter ─┘          │                └─> Tool registry / MCP
                                               ├─> session + transcript 存储
                                               ├─> prompt / AGENTS.md / skills
                                               └─> 统一内部事件

Gofer 拥有 job、worker、项目、日志、重试和 Web；Suag 拥有 Agent turn 与对话状态。
```

个人会话模块应提供一个小接口，例如 `Run(ctx, {session_id, prompt, cwd, policy}, emit) -> {session_id, output, status}`。调用者不必知道内部 `Task`、`Team`、`ExternalTask` 或三层状态机；错误、取消、等待输入必须有可区分的结果。CLI 与 ACP 两个 adapter 是这个接口的首批真实调用者。会话实现先放在 `internal`；已有 `pkg/agentcore` 保持可嵌入，暂不增加另一套公开 Go 接口。

## 关键流程

1. **独立使用**：`suag run --prompt "..." --output-format stream-json` 打开或创建 session，输出第一条 `session` 事件，再按顺序输出有限的 turn/tool 事件，最终只输出一条含 `result` 文本的终态事件；错误写结构化错误事件并返回非零退出码。`suag chat` 复用相同的个人会话模块。
2. **Gofer 批处理**：Gofer 按 `cli-agent` argv 模板启动 `suag run`，用 `session_inject` 传入 id；Suag 的 `session` 事件让 Gofer 捕获会话，`ndjson_stdout_path: result` 把最终答复送到 Gofer stdout。续接使用 `session_resume` 调用 `suag run --resume <id> --prompt ...`，缺失或损坏的 session 应明确失败，不静默新建。
3. **Gofer ACP 交互**：Gofer 以 `type: acp-agent` 启动 `suag acp`。`initialize` 只声明已实现能力；`session/new` 创建持久会话，`session/load` 恢复已有会话，`session/prompt` 调用同一个个人会话模块并发送 `session/update` 文本与工具事件。Gofer 的常驻 `--session` 可在同一进程多次 prompt；`job resume` 可由新进程 `session/load` 续聊。ACP 的 `session/load` 响应按协议无需回带 id，原请求 id 为被加载会话。
4. **ACP 过程控制**：`session/cancel` 取消当前 turn；`session/set_mode` 提供可验证的 `read_only` mode；写文件/执行命令前通过 `session/request_permission` 请求 Gofer 的过程审批，拒绝或超时不得执行工具。审批与工具事件从 `agentcore.BeforeTool/AfterTool` 映射；ACP stdio 的 stdout 仅写 JSON-RPC 帧，诊断日志写 stderr。
5. **进程重启续跑**：新进程从持久 transcript 恢复对话，不依赖 `intrunner.sessions` 的内存 history。先验收已完成 turn 的后续提问；等待中的工具/审批恢复需单独验证，不能以“能加载 session 列表”替代。
6. **工具与技能**：默认内置少量文件/搜索/命令工具；从当前 registry 装配可选 MCP tools，从现有目录发现技能。ACP 客户端传入的 `mcpServers` 只通过允许的现有 MCP provider 装配；不支持的传输明确拒绝，不静默忽略。对每轮模型暴露经过 policy 过滤的工具集合；skill 提供指令和资源，不另开执行循环。

Gofer 配置的目标形态如下。`PROMPT_TOKEN`、`SESSION_ID_TOKEN` 是设计占位名称，实施时替换为 Gofer 的 argv 模板变量；字段须用当前 Gofer `config validate` 和真实 job 验证：

```yaml
agents:
  suag-cli:
    type: cli-agent
    command: suag
    args: [run, --output-format, stream-json, --prompt, PROMPT_TOKEN]
    session_inject: [--session-id, SESSION_ID_TOKEN]
    session_resume: [run, --output-format, stream-json, --resume, SESSION_ID_TOKEN, --prompt, PROMPT_TOKEN]
    read_only_args: [--read-only]
    output_format: ndjson
    ndjson_keep: [session, turn_start, tool_start, tool_end, result, error]
    ndjson_stdout_path: result
  suag-acp:
    type: acp-agent
    command: suag
    args: [acp]
    acp:
      modes: {read_only: read_only}
      permission_policy: ask
```

ACP 模式优先在 `suag-acp` 的配置中设置过程审批 `permission_policy: ask`，只收紧这个 Agent，不改变同一 Gofer 项目下其他 Agent 的权限；专用项目也可配置 `approval.mode: ask`。审批策略属于 Gofer 配置，不能靠 Suag 的提示词替代。

## 安全、数据、运维与回滚

- Headless CLI 模式绝不等待终端确认。`--read-only` 与 ACP `read_only` mode 必须在工具执行层禁止写入和命令副作用，而非仅修改提示词；首切片可直接排除 `file_write` 和通用 `shell_exec`。写权限只在显式信任的工作目录和允许的工具范围内开放。CLI 无法处理中途人工批准时应明确拒绝，ACP 通过 `session/request_permission` 与 Gofer 过程审批衔接；Gofer 的结束后 `--review` 不等于执行前批准。
- 会话与 transcript 继续由 Suag 保管，Gofer 只持有 job/session 关联和日志；事件、日志、recap 不写凭据、完整敏感工具结果或原始环境变量。Gofer job id 与 Suag session id 不混用。
- 只读限制作用于 Agent 工具和工作区文件；会话与运行记录仍写入 `~/.suag/db`，以支持新进程续聊。用隔离的用户主目录测试默认路径、显式覆盖和仓库 `db/` 不被默认新入口写入；真实用户目录中的旧数据不用于测试。
- 先保持当前 SQLite schema；迁移须另附兼容与备份方案。删除旧 Web 前，先以 CLI/ACP 验证保留的用户路径；切换前保留现有源与数据库备份点，失败可用 Git 恢复源代码。`web/app`、Web gateway/AG-UI、`server` 命令及对应无效测试在验收后删除。
- `legacy/` 使用独立的嵌套 Go module 隔离旧 `team/cron/orchestrator`、不再需要的 `internal/agent` 适配和出站 ACP bridge；只在 active import 清零后移动。根模块的 `go list ./...`、构建和测试必须不包含 `legacy/`，目录 README 明示仅供阅读、不承诺可运行或维护。
- Gofer 可作为调度入口，但 Suag 不自动注册 Gofer MCP 作为工具，避免 `Gofer -> Suag -> Gofer -> Suag` 递归与意外任务提交。反向调用是后续独立决策。

## 决策

| 项 | 设计选择 | 理由 |
|---|---|---|
| 产品定位 | 个人 Agent，先服务本人的项目任务 | 以可使用的单 Agent 闭环替代平台口号 |
| 首要入口 | 无服务 CLI + ACP stdio；Gofer 用 `cli-agent` 批处理、`acp-agent` 交互 | 复用 Gofer 已有控制面与 ACP 常驻会话 |
| 执行内核 | 保留 `pkg/agentcore`，消除旧 runner 绕行 | 已有测试与持久 transcript，避免第二套循环 |
| 扩展 | Provider、Tool/MCP、skill 组合点 | 使用现有模块，暂不发明插件 ABI |
| 状态 | Suag session 为对话真源，Gofer job 为执行真源 | 支持续跑且避免双写控制状态 |
| 个人会话默认目录 | `~/.suag/db`；保留显式路径覆盖，不自动迁移仓库 `db/` | 按用户指定的数据目标隔离工作树与运行状态，保留旧数据访问方式 |
| Web/legacy | 删除旧 Web；旧控制面代码移到不参与构建的 `legacy/` | 主仓库只保留个人 Agent 活跃路径 |
| ACP 实现 | 新增入站 ACP Agent adapter，优先评估 Go SDK | 现有 `acpbridge` 是出站客户端，不能充当 Gofer 所需的服务端 |

## 待确认事项

1. 首切片的任务侧重是否仍以项目/编码任务为主？本草案按“个人通用 Agent，先验收项目任务”处理；若要优先做日程/消息等个人事务，需要调整默认工具与验收。
2. Suag 现有 SQLite 会话数据若仍需使用，可通过显式路径读取；本阶段保留 schema 与原文件，不做自动迁移。
3. ACP Go SDK 与当前 Gofer 的协议组合是否覆盖 `session/load`、mode、permission 和取消，须在实施计划的首个协议试验中实测；失败时重新评估具体实现，不静默降级为无审批的 CLI。

## 结论与人工计划 Gate

先以 **共同会话内核 + CLI 批处理 + ACP 常驻交互 + Gofer 双入口** 验证个人 Agent。后续正式实施计划应分为：协议试验与会话主链、CLI 合同、ACP mode/permission/续跑、Gofer 真实联调、旧 Web 删除与 legacy 隔离。首切片至少演示只读项目问答、受信任目录的受批工具任务、ACP 同进程第二轮、Gofer job 结束后新进程 `session/load` 续接；拒绝审批不得执行工具，只读模式不得写文件。根模块测试与构建排除 legacy，Web 删除后不能留下失效导入。每阶段以真实用户路径和相应自动检查验收；通过单元测试但未跑通 Gofer ACP job，不算首切片完成。

本设计属于 Full 调整。用户于 2026-10-06 对上一轮方案回复“好的”，同时明确要求 ACP 交互、Web 删除、旧 team/cron/orch 移入不使用的 legacy，以及本次试用 Gofer issue/memory；上述内容构成进入规划的具名范围。设计核对了现有 Suag/Gofer/ACP SDK 接口，未发现核心阻断。逐文件实施计划须另行评审与人工确认；本设计确认不授权代码重构。
