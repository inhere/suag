<!-- template_id: plan; template_version: 1.2.0 -->
# Suag 个人 Agent 收敛实施计划

> 状态：Draft 0.3 / 数据目标修订待人工计划批准

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-06 | Codex | 规划共同会话主链、CLI/ACP 双入口、Gofer 联调及旧控制面退出 |
| 0.2 | 2026-10-06 | Codex | ACP 审批限定 Suag Agent，调整 Web 删除与 legacy 移动顺序 |
| 0.3 | 2026-10-07 | Codex | 将个人 CLI/ACP 默认持久会话放入 `~/.suag/db`，补齐旧数据与只读验收 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

## 目标与完成定义

本计划使用 `RIGOROUS`。核心目标是让 Gofer 通过 `cli-agent` 执行 Suag 一次性任务，并通过 `acp-agent` 常驻交互、过程审批和跨进程续聊；Suag 本地 CLI 使用同一个 Agent 内核。范围冻结为 Suag 单 Agent 源码、旧 Web 删除、legacy 隔离、Gofer 配置与真实联调。新增渠道、多 Agent、自动学习平台和 Gofer 源码改动均延后；发现超出范围的接口、数据或安全变化时停止并修订。审核预算是一轮事实核对、一轮确认，只有核心阻断才继续修正。

完成必须同时满足：

1. `suag run` 输出可被 Gofer 捕获的实时事件、最终答复与失败状态；`suag chat` 复用同一会话模块。
2. `suag acp` 完成 `initialize/new/load/prompt/cancel/set_mode/update/request_permission` 的适用合同；Gofer `job run --session`、`job say/end` 和新进程 `job resume` 真正运行并保留上下文。
3. ACP 与 CLI 的只读 mode 不写工作区文件或执行有副作用的工具；会话仍持久化于 `~/.suag/db`。编辑或执行工具的权限请求被拒绝时工具不运行。CLI 无法交互审批时明确失败，不挂起等待 stdin。
4. 旧 Web 前后端和 `server` 入口已删除；旧 team/cron/orchestrator 等只留在根 Go module 不参与构建的 `legacy/`；根模块 `go list ./...`、`go test ./...`、`go build ./cmd/suag` 通过。
5. Gofer issue/memory 可记录与恢复进度；真实 Gofer 作业验收与单元测试分开报告。资料、配置和运行日志不含凭据。

## 范围、排除项与授权

- 代码归属：独立 Git 根 `D:/work/inhere/my-tools-dev/inhere-tools/suag`；Gofer 源码根 `D:/work/inhere/my-tools-dev/gofer` 只读。目标主机 Gofer 配置 `D:/work/inhere/config/win-env/gofer/config.yaml` 仅在联调波次按单独 Gate 调整。
- 保持现有 SQLite session/run 数据的读取兼容；本计划不实施数据库迁移、不清理已有会话数据、不发布或推送代码。Pigo 仅作设计参考，不引入为依赖。
- 个人 CLI/ACP 的默认会话数据库为 `~/.suag/db/suag.db`，run/transcript 根为 `~/.suag/db/agentcore/host`；`~` 通过用户主目录解析。显式 `database_url`、`agentcore.run_root` 继续优先；旧 Web 在删除前保留原默认目录。已有仓库 `db/` 不自动迁移或覆盖。
- 当前用户明确要求本次不用 `bd`；进度与后续事项使用 Suag 仓库的 `.gofer/tracker`、`gofer issue` 和 `gofer memory`。已置 `auto_sync: false`，不把嵌套仓库记录自动镜像到父项目。
- 本计划包含 Gofer 主机配置与真实 job 运行，故外部动作适用性为 `host_or_non_offline_action=REQUIRED`。计划批准不自动授权修改该主机配置、启动 Agent 作业或执行其他出机器动作。

## 输入与批准证据

- 输入设计：[2026-10-06-personal-agent-redesign-design.md](../design/2026-10-06-personal-agent-redesign-design.md)，修订 `0.4`；用户在 2026-10-06 对原设计回复“好的”，并明确增加 ACP 交互、删除 Web、移动旧 team/cron/orch 至 legacy、改用 Gofer issue/memory；2026-10-07 又明确选择 `~/.suag/db` 作为默认数据目录。上述决定固定设计范围，修订后的本计划仍待批准。
- 当前执行请求与计划批准证据：用户在审阅本计划 `0.2` 后要求“执行一下 gofer repo sync 同步issue memory 上 server 我看看。先在当前commit 打一个 v0.1.0 tag 记录；先做一点调整，将 chi 切到 gookit/rux/v2 库，之后再开始计划”。这明确给出了顺序和本计划的当前执行意图；先行三项动作不扩大 T01–T08 的目标，T05 的主机配置与真实 Agent 作业仍受单独 Gate 约束。
- 设计审查：直接核对 Suag 的旧 CLI/Host/ACP client、Gofer 的 `acp-agent` runner/config、ACP Go SDK 的 Agent 侧接口，设计文档 validator PASS。若 SDK 与本机 Gofer 不兼容，S0 停止后回设计 Gate。
- 跟踪：`gofer issue show suag-ha0z` 是主事项；`suag-xpks` 记录 tracker 初始化的嵌套仓库同步体验；`gofer memory show suag-personal-agent-scope` 是用户已定范围摘要。它们不替代本设计或当前授权。

## Capability Discovery

### Capability decisions

| capability_id | required_capability | searched_candidates | direct_reuse | thin_adapter_or_owner_extension | decision | proven_gap | duplication_and_lifecycle_risk |
|---|---|---|---|---|---|---|---|
| CAP-01 | 跨 CLI/ACP 的持久个人会话执行 | `internal/host/session`、`intrunner`、`pkg/agentcore`、Pigo Session | agentcore 可复用循环和 transcript，Host session 可复用 SQLite 索引 | 直接给 CLI 包装旧 `intrunner` 仍依赖内存 history 和旧 Agent；扩展 Host service 会继续携带 Web/ExternalTask 语义 | MINIMAL_NEW_MODULE | 需要一个共享会话 owner 统一新 run、历史重建、policy 和事件，现有入口没有此小接口 | 仅 `internal/personal` 拥有个人会话生命周期，旧 Host 主链在退出波次删除或收缩 |
| CAP-02 | Gofer 可调用的 ACP Agent stdio | 现有 `internal/host/acpbridge`、Gofer `internal/runner/acp`、`coder/acp-go-sdk` | 现有 bridge 是客户端，Go SDK 可复用 JSON-RPC/ACP 类型和传输 | 在已有 `cmd/suag` 上接一个薄 adapter 仍需持有连接、session/mode/permission 映射 | MINIMAL_NEW_MODULE | Suag 没有入站 ACP Agent 实现；需隔离协议状态与共享会话状态 | `internal/acpserver` 只拥有协议 adapter，业务状态归 `internal/personal`；不用第二套 Agent 循环 |
| CAP-03 | Headless CLI 与可读进度 | `cmd/suag`、`internal/app/chat.go`、Gofer NDJSON generic projector | 现有命令入口与 Gofer argv/session 模板可复用 | 在 CLI owner 扩展 `run/chat` 和 stream-json emitter | OWNER_EXTENSION | 现有 chat 走旧 orchestrator，暂无 Gofer 可用输出合同 | CLI 仅映射共享事件；不新建独立调度器或 reporter |
| CAP-04 | 工具审批与只读 | `pkg/agentcore` BeforeTool/AfterTool、`ToolExecutionPolicy`、Gofer `session/request_permission` 与 modes | 现有 hook 与工具注册可复用 | 在个人会话 policy 中拦截执行前工具调用，ACP adapter 请求许可 | THIN_ADAPTER | CLI 无中途审批通道，须拒绝；ACP 需将许可结果映射到现有 hook | policy 只保留一个 owner；拒绝和取消在执行前终止，不复制 Gofer 审批记录 |
| CAP-05 | Provider、MCP、技能扩展 | `internal/platform/llm`、`internal/platform/tools/mcp`、`internal/capability/skills` | 现有 provider/tool/skill 能力足够首切片 | 由个人会话模块装配 ACP `mcpServers` 与已允许工具 | DIRECT_REUSE | 当前无第二个插件 ABI 用例 | 不引入 Pigo、插件市场或动态加载系统 |
| CAP-06 | 删除 Web 并隔离旧控制面 | `web/app`、`internal/host/gateway`、`agui`、旧 team/cron/orchestrator、Go nested module | Git 移动和 Go module 边界可直接完成 | 先清 active import，再移动旧源码并删 Web | DIRECT_REUSE | 无新运行机制缺口，只有依赖切断与资料归档 | legacy 不参与根构建；不让旧模块以 adapter 名义继续活跃 |
| CAP-07 | Gofer 作业、会话、记录 | `gofer cli-agent/acp-agent`、`gofer issue/memory`、项目配置 | 直接复用现有 Gofer 项目与作业控制面 | 只增加 Suag agent 配置和最小联调说明 | DIRECT_REUSE | 当前服务尚无 `suag-cli`/`suag-acp` 注册 | 不改 Gofer 源码，不另建 job scheduler 或 Web |
| CAP-08 | 个人会话默认目录与旧数据兼容 | `internal/config/paths.go`、显式路径配置、Go `os.UserHomeDir` | 保留现有 SQLite 仓储和显式覆盖配置；标准库解析用户主目录 | 在个人入口的配置 owner 增加默认路径解析，旧 Web 默认值保持到删除 | OWNER_EXTENSION | 相对 `db/` 默认值不符合用户指定的 `~/.suag/db`，无需新存储模块 | 不复制 session schema、不迁移旧 `db/`；CLI/ACP 只共享一套路径解析 |

### New module candidates

| candidate_id | capability_id | proposed_module | searched_candidates | direct_reuse_gap | thin_adapter_or_owner_extension_gap | proven_gap | unique_owner_and_lifecycle | deletion_or_merge_handling |
|---|---|---|---|---|---|---|---|---|
| CAND-01 | CAP-01 | `internal/personal` | Host session service、intrunner、agentcore | 旧 Host 带 Web/ExternalTask 状态且进程内 history 不支持已完成 turn 的跨进程上下文 | CLI/ACP 两处分别装配会复制会话状态；继续扩展 Host 会保留旧语义 | 需一个共享的个人 session 到 run 映射与事件 owner | 个人会话唯一 owner；CLI/ACP 只依赖它，旧 intrunner 导入清零后归档 | 若现有 session owner 可收缩到同样小接口，合并进 session 并删除此新 Module |
| CAND-02 | CAP-02 | `internal/acpserver` | 出站 acpbridge、Gofer client、ACP Go SDK | 无入站 ACP Agent；SDK 只提供协议类型和传输 | 把 stdio 请求直接写入 CLI 会混合协议连接与业务生命周期 | 需持有 ACP 连接、session/mode/permission 映射而不污染个人会话模块 | 只拥有 ACP 协议生命周期；由 `cmd/suag acp` 创建，进程退出即释放 | 若 SDK 的 Agent 实现可直接落到 cmd 下且不扩散，折回 cmd 并删除此 Module |

### Rejected new tools

| rejected_candidate | capability_id | deletion_test_and_reason |
|---|---|---|
| 自制 ACP JSON-RPC 栈 | CAP-02 | 删除后 SDK 的 Agent 侧 stdio 与 Gofer 兼容试验仍可覆盖协议；只有试验失败才重审 |
| 新插件框架 | CAP-05 | 删除后现有 Provider、Tool/MCP、skills 可完成两个真实入口的首切片 |
| 新 Web/作业调度器 | CAP-07 | 删除后 Gofer 已提供交互、作业、日志与项目/Worker 管理 |

## 前置检查与 fail-closed 条件

- Baseline：Suag Git root 如上，branch `main`；改造前 HEAD `8c84aa9465b5521a1b2ec196a0f10e87fd3a8e52` 已由本地注释 tag `v0.1.0` 固定。用户指定的计划前 chi → rux/v2 调整已单独提交 `6ff259b`，并通过 `go test ./...`；chi 原本仅供 T06 将删除的旧 Web gateway 使用，T06 不因这个先行提交保留 gateway。随后 `git status --short --untracked-files=all` 仅有本次创建的 `.codex/hooks.json`、`.gofer/.gitignore`、`.gofer/tracker/{config.yaml,issues.jsonl,memories.jsonl}`、设计与计划文档。根工作区另有原有 dirty/untracked，特别是 `workspace.md`；不得触碰或随 Suag 提交。
- Gofer tracker 已按用户指令执行一次 `gofer repo sync` 到 server 项目 `my-tools-dev`，当时 `pending_sync: 0`；本地 `auto_sync: false` 保持，后续进度若需服务器可见须显式同步。tag 与 rux 先行提交均不代表 T01–T08 已开始或通过。
- Gofer 源码只读 HEAD `2ef84014ef945c7289a28ad2c650d1af4f7ac2e3`，本机 CLI 报告 `0.81.0-1-g11aef54`。实施前重新运行 `gofer --version`、`gofer job run --help`、`gofer config validate server`；不能由源码 HEAD 推断本机 CLI 已具相同合同。
- Suag 当前 `go test ./...` 于 2026-10-06 PASS；实施前复测。检查 Go 1.25、ACP SDK 版本与 Gofer 协议版本。缺 SDK/Gofer 兼容证据、缺会话数据备份路径、Gofer 配置无法验证或 read-only/permission 无法阻止副作用时，停止对应波次。
- 实施前重查嵌套 Git 根、HEAD、dirty 与数据库路径；既有 `db/suag.db` 及 `db/agentcore/**` 视为用户数据，不在测试和迁移中覆盖。测试使用临时目录。Codebase Memory 图服务本次 `list_projects` 返回 `Transport closed`，相关 owner 与导入需以当前源文件和 `go list` 再核实。
- 恢复发现父工作区 IDEV-STD 已从 `0.22.2` 升至 `0.23.0`，旧 SUPMODE 指纹 `s2:ffhB3PoqDrQV3h53vb35ug` 失配；T01/T02 已验收，T03 checkpoint `f673084` 与测试修正 `e4654d0` 未构成 T03 完成。须在本修订获批后重新 preflight，不能从旧指纹直接续跑。

## 波次与依赖

| 波次 | 任务 | 前置 | 可交付结果 |
|---|---|---|---|
| W0 | T01 | 计划与当前执行请求 Gate | ACP SDK 与 Gofer 合同的最小试验结论 |
| W1 | T02 | T01 | 同一持久个人会话主链 |
| W2 | T03、T04 | T02 | CLI 与 ACP 两个真实入口及安全合同 |
| W3 | T05 | T03、T04 | Gofer 双入口、常驻与续接运行证据 |
| W4 | T06 | T05 | 删除旧 Web 及对出站 ACP bridge 的引用 |
| W5 | T07 | T06 | legacy 隔离旧控制面 |
| W6 | T08 | T07 | 文档、完整质量门和交接 |

## 任务

### T01 ACP 兼容试验

- 文件/owner：`go.mod`、`go.sum`、新 `internal/acpserver/` 的最小协议试验文件；Gofer 源码与主机配置只读。
- 动作：用 `coder/acp-go-sdk` Agent 侧 stdio 接口做 initialize/new/load/prompt/set_mode/cancel/permission 的最小本地试验；只声明实现的能力，stdout 保持纯 JSON-RPC。确认 SDK `session/load` 响应和 Gofer client 的版本/字段契约。失败时记录首个不兼容方法与原始类型，不切成无审批模式。
- 验证：`go test ./internal/acpserver/...`；`go test ./...`；保存方法级测试结果到 `gofer issue suag-ha0z` notes。
- 完成标准：本地 ACP client 与服务端完整走过指定方法、取消不挂死；Gofer 真实进程联调留到 T05。
- 依赖：计划批准及当前执行请求。

### T02 收敛持久个人会话主链

- 文件/owner：新 `internal/personal/`；`internal/host/session/{repository.go,service.go,service_execution.go,sqlite*.go}` 中必要的存储接口；`pkg/agentcore` 的现有公共 contract 与 SQLite storage，仅在证实缺口时改动。
- 动作：给 CLI/ACP 共用的小接口实现会话打开、turn 运行、事件和 policy。完成的 turn 使用新 RunID，重建上轮 transcript/context；yielded run 仅按现有 ResumeMode 续其未终结 RunID，绝不把 terminal run 当作可 resume。跨进程加载失败不得新建伪同名 session；不改现有数据 schema。
- 验证：`go test ./internal/personal/... ./internal/host/session/... ./pkg/agentcore/...`；临时 SQLite 目录中运行两轮并重启进程后再问上轮事实。
- 完成标准：两个入口可使用同一会话模块；跨进程继续保留上下文；原有 session 数据仍可读；失败与取消状态可区分。
- 依赖：T01。

### T03 CLI 批处理和交互入口

- 文件/owner：`cmd/suag/main.go`、`internal/app/{app.go,chat.go,cli.go}`、`internal/config/paths.go` 及对应 focused tests、`README.md`。
- 动作：提供 `run --prompt --session-id/--resume --output-format stream-json --read-only` 与 `chat`；二者调用 T02。stream-json 第一条为 session，最终一条 result；Gofer 通用 NDJSON 可提取答复。个人 CLI 默认将 SQLite 与 run/transcript 写到用户主目录 `~/.suag/db`，显式路径覆盖仍可用；旧 Web 默认路径不变且不迁移旧 `db/`。CLI 无 stdin 审批时清楚拒绝；旧 `orchestrator.RunTask` 不再用于新入口。
- 验证：`go test ./cmd/suag ./internal/app/... ./internal/config/... ./internal/personal/...`；隔离用户主目录下的默认路径、显式覆盖及仓库 `db/` 不被新入口默认写入测试；mock provider 命令 smoke；`go build ./cmd/suag`；Docker `golang:1.25` focused `-race`。
- 完成标准：可在无 Web 服务时执行、连续输出、续聊、失败非零退出；只读模式对工作区写文件和 shell 工具真正拒绝，同时会话写入 `~/.suag/db`；原仓库 `db/` 不受默认新入口影响。
- 依赖：T02。

### T04 ACP 常驻、只读与审批

- 文件/owner：`internal/acpserver/`、`internal/personal/`、`cmd/suag/main.go`、必要的 `pkg/agentcore` hook/policy 文件及 focused tests。
- 动作：`suag acp` 接 Gofer 协议；`session/new/load/prompt/cancel/set_mode` 共用 T02；发送文本/工具 `session/update`，执行前对 edit/execute 申请 `session/request_permission`。对拒绝、超时、取消作 fail-closed 处理；将 ACP `mcpServers` 映射到现有允许的 MCP provider，不支持的传输报错。ACP stdout 只用于协议。
- ACP 默认会话与 CLI 使用 T03 的同一 `~/.suag/db` 路径解析，并在隔离用户主目录中验证新进程 `session/load`。
- 验证：`go test ./internal/acpserver/... ./internal/personal/... ./pkg/agentcore/...`；使用 SDK client 的两轮、load、read-only、permission allow/reject、cancel 用例；`go build ./cmd/suag`。
- 完成标准：拒绝审批无副作用；read-only 不执行写/命令；第二轮和新进程 load 均保留上下文；取消使正在运行的 turn 有界退出。
- 依赖：T02、T01。

### T05 Gofer 双入口真机联调

- 文件/owner：Suag `docs/usage/` 的 Gofer 接入说明；主机 `D:/work/inhere/config/win-env/gofer/config.yaml` 的 `agents.suag-cli/suag-acp`，以及仅在准入要求时的 `projects.my-tools-dev.allowed_agents`；Suag 源码不在此任务修改。
- 动作：先备份配置并校验候选，经过外部动作 Gate 后登记 `suag-cli`、`suag-acp`；对 `suag-acp` 设置 `acp.permission_policy: ask`，只收紧此 Agent，不改变 `my-tools-dev` 项目下其他 Agent 的审批模式。按 Gofer 支持的配置 reload 让当前 server 生效。使用 `--runner server --project my-tools-dev --cwd inhere-tools/suag` 提交批处理与 ACP 常驻 job；调用 `gofer job say/end`、`job resume`，分别检查结果、工具事件、只读与权限拒绝。凭据只引用环境或 Gofer 已有安全配置，不写入 agent argv、任务或日志。
- 验证：`gofer config validate server`、`gofer agent list`、`gofer project show my-tools-dev`、`gofer job show/logs <id>` 与真实运行记录。若 reload 不支持该配置项，停止并按现有 Gofer runbook 判断有界重启，不能假定生效。
- 完成标准：Gofer 两类 Agent 均可提交；ACP `--session` 同一 job 第二轮成功、新进程 resume 成功；审批拒绝和只读失败边界在 Gofer 中可观察；源码/本机 CLI 版本差异已核对。
- 依赖：T03、T04；主机配置与真实 Agent 作业的外部动作 Gate。

### T06 删除旧 Web 路径

- 文件/owner：`web/app/**`、`internal/host/gateway/**`、`internal/host/agui/**`、`internal/app/{server.go,server_test.go,app.go}`、`cmd/suag/main.go` 的 `server` 命令、旧 Web 专用 `docs/usage/web-host-local-dev.md` 与相关 README/AGENTS 入口。
- 动作：以 T05 的 Gofer ACP/CLI 用户路径为替代验收后，从 Suag Git 根删除 Web 前后端入口和已无效测试；清除 `app.go` 中旧 gateway/orchestrator/team/cron 的初始化导入。当前 `gateway` 测试依赖出站 `acpbridge`，因此 Web 删除必须先于 T07 的 bridge 移动。历史设计文档保留在 docs/archive 或修正索引。删除前列出并核对精确路径；用户数据目录不在删除集。
- 验证：`rg --files web/app` 无活跃文件；`rg` 检查 `server`/gateway/agui 的 active import；`go test ./...`、`go build ./cmd/suag`；Gofer 常驻 job 再跑一次基础问答。
- 完成标准：Suag 不提供自己的 Web 控制面，根模块无死链接/死导入，Gofer 交互仍可用；旧控制面包暂留原位供 T07 移动。
- 依赖：T05。

### T07 隔离旧运行控制面

- 文件/owner：`internal/agent/**`、`internal/orchestrator/**`、`internal/team/**`、`internal/ops/cron/**`、`internal/host/acpbridge/**`，经导入核对确认无新主链依赖的 `internal/host/intrunner/**` 与 `internal/ops/hitl/**`；`internal/host/session` 中旧 ACP bridge 集成测试；新 `legacy/go.mod`、`legacy/README.md`、`legacy/internal/**`。
- 动作：先通过 `go list -deps ./cmd/suag` 与源引用确认 active import 清零；将仍引用出站 bridge 的旧 session 测试一起隔离或重写为当前合同测试。在同一个 Suag Git 根内逐个移动旧模块到嵌套 Go module，保留历史参考。Windows 移动前确认解析后的源与目标绝对路径均位于 Suag Git 根；不得跨 shell 拼接路径。无法清除依赖的模块保留活跃并按发现分类，不强行移动。
- 验证：`go list ./...` 不列 legacy；`go test ./...`、`go build ./cmd/suag`；对旧 import path 做限于 active 源码的 `rg` 检查。
- 完成标准：旧 team/cron/orch 和已失效适配不在根模块依赖图，legacy README 标明仅供参考且不承诺可运行；个人 Agent 运行结果不变。
- 依赖：T06。

### T08 完整验收与资料收口

- 文件/owner：Suag `README.md`、`AGENTS.md` 与 `CLAUDE.md` 的实质性同步、`docs/usage/` Gofer 接入文档、`go.mod/go.sum`、`.gofer/tracker/issues.jsonl` 与 `memories.jsonl`；不改 Gofer 源码。
- 动作：清理未使用依赖、使 README 只描述当前 CLI/ACP；记录 CLI/ACP/Gofer 的真实证据和风险，更新 Gofer issue 主事项的阶段结果。仅在实际完成并获允许的生命周期节点关闭 `suag-ha0z`；试用发现 `suag-xpks` 保持独立跟踪。
- 验证：`go mod tidy` 后 `go test ./...`、`go build ./cmd/suag`、`go list ./...`；必要时 CLI mock smoke 和 T05 Gofer 运行记录复核；`git diff --check`、两个 Git 根 `git status --short`。
- 完成标准：源、文档、真实 Gofer 运行与 tracker 状态一致；按功能点报告变更、验证和未完成项，未经授权不推送或发布。
- 依赖：T07。

## 回滚与恢复

- 每波次保留上一波次已验证的 Suag Git 状态与可恢复数据库副本；代码路径切换先验证新入口，Web/legacy 清理最后进行。若权限或会话持久化失败，恢复到前一已验证的活跃入口并保留失败证据。
- Gofer 配置变更须保存原文件且只编辑已确认字段；reload 后若 Agent 不可用，恢复原配置并 reload，避免对运行中无关 job 做无依据重启。Gofer tracker `auto_sync:false` 保持本地，未经明确选择不把记录镜像到父项目。
- 每阶段 `gofer issue update suag-ha0z --append-notes` 记录实际路径、测试与阻断；文档不是任务状态真源。实施中发现新 path/symbol，按 `Operational Discovery / Corrective / Semantic Amendment / Ownership Conflict` 分类后再改；用户或其他 Agent 的 dirty 文件一律停在 ownership 冲突。

## 人工 Gate

1. 本计划需用户明确批准；之后还需一次当前明确的执行请求，才可开始 T01 的源码修改。当前消息仅确认设计方向与 Gofer tracker 试用。
2. T05 修改主机 Gofer 配置、reload/重启或启动真实 Agent job 前，按外部动作 Gate 列出具名目标并取得当次授权；真实凭据动作另行确认。用户已授权的本地 Gofer issue/memory 试用不扩展为这些动作的授权。
3. 数据 schema/安全/ACP Interface、任务范围或验收变化时回 design/plan Gate。删除 Web 与 legacy 移动已由用户提出，但仍依赖本计划的通过和执行请求。

## 可追溯性

| 用户结果/决定 | 任务 | 验证 |
|---|---|---|
| 个人单 Agent、简化主链 | T02、T03、T06 | 共同会话测试、`go list ./...` |
| 默认会话位于 `~/.suag/db` 且旧数据不迁移 | T03、T04 | 隔离用户主目录路径、显式覆盖与新进程 load 测试 |
| Gofer 批处理执行 | T03、T05 | NDJSON 答复与 Gofer job 日志 |
| Gofer ACP 常驻与跨进程续聊 | T01、T02、T04、T05 | `job say/end/resume` 与 session/load 上下文 |
| 只读和过程审批 | T03、T04、T05 | 拒绝无副作用、只读不写、Gofer permission 记录 |
| 删除旧 Web、保留 legacy 参考 | T06、T07、T08 | 文件/导入扫描、构建/测试、README |
| Gofer issue/memory 替代本次跟踪 | T08 | `gofer repo status/prime`、issue/memory readback |

## 完成 Gate 与剩余工作

仅当 T01–T08 的功能与受影响检查都通过、T05 的真实 Gofer 证据存在、Web/legacy 状态达到设计目标、用户数据未受损，才能将 `suag-ha0z` 关闭并报告实施完成。单元测试、文档 validator 或 Gofer 健康检查单独为绿均不代表用户路径验收。未完成项另记 Gofer issue 并报告首个阻断；Gofer tracker 试用问题 `suag-xpks` 不因 Suag 功能完成而自动关闭。
