# 终端与 Web 同会话交互调研（2026-10-14）

issue：suga-lrqy（`web: 通过 Web UI 页面与 Agent 交互`）。触发问题（用户原话）：

> 如果要做 suga web，是否可以做到不关闭终端会话，web 也可以进入同会话输入，要保证始终只有一个可写，读到的都是最新的信息。

本文回答三件事：**能否同会话**、**单写者怎么保证**、**另一端怎样读到最新**。结论来自对源码与 tracker 的静态核对（基线 `7046c5d`，2026-10-14），**没有运行时验证**；本文是调研记录，不是实现合同，实施前按 AGENTS.md 补 `docs/design/` 设计文档与计划。

## 判定标准

要把问题拆成可判定的三条：

1. **单写者**：任何时刻一个会话只能有一个回合执行者（Runner）在写条目、事件与状态。
2. **读到最新**：另一端能看到进行中回合已经发生的事情（消息、工具调用、审批、回合状态）。
3. **可输入**：另一端能发消息、回答审批与提问、中止当前回合。

## 结论速览

- **能做**，且"单写者"不需要新协议：会话级互斥已有三层（进程内闸门 + 跨进程文件锁 + 持久 `status=running`，`internal/personal/gate.go:10-26`）。
- 真正的两个缺口是：**流式读取**（模型 delta 不落库，且消费者各自 `Emit`、没有事件总线）与**审批/提问的所有权**（现在默认"终端问、终端答"）。
- 一条硬约束：web 侧**不得直写 store**。`sessions`/`turns` 没有版本号或 CAS（`internal/personal/store/sessions.go:154-164`、`turns.go:88` 盲写），绕过 Runner 的写入会同时绕过三层锁。

| | A. 只做第二输入面 | B. 同进程两前端 | C. web 自己跑回合 |
|---|---|---|---|
| 形态 | web 不跑回合，输入经 socket（即 `suga send`）投给正在运行的 chat | `suga chat --web` / `suga web --session`：HTTP 服务起在 chat 进程内，终端与浏览器是两个前端 | `suga web` 内直接调 `personal.Run` |
| 单写者 | 天然（写者只有 chat 进程） | 进程内输入所有权仲裁 | flock 保证与终端互斥（终端 `chat --resume` 被拒） |
| 终端 + web 同时用 | 可以，但只能 follow-up（回合结束后才被消费） | 可以，真正同会话 | **不行**（与需求冲突） |
| 代价 | 最小；审批仍在终端 | 需抽象输入源 + 会话级提问/审批事件 | 最省；语义变成"独占/领养" |
| 与本项目标 | 第一期 | **目标形态（推荐）** | 第三期 |

## 现状盘点（代码事实）

### 会话与数据

- 单个 SQLite 库 `~/.suga/db/sessions.db`，DSN 每个池化连接带 `journal_mode(WAL)`、`busy_timeout(15000)`、`synchronous(NORMAL)`、`foreign_keys(1)`（`internal/personal/store/store.go:65-77`）；**没有只读句柄**（无 `mode=ro`），`DB()` 供同包查询（`store.go:108-109`）。
- 条目在回合进行中**逐条落库**（`pkg/agentcore/runtime.go:113-121` 用户消息、`:583` 每步 assistant + tool、`:743-745` 结束），`AppendEntries` 先写 sessions 再取 `MAX(seq)+1`（`internal/personal/store/entries.go:26-47`）。
- 事件**实时落库**（best-effort，5s 超时，`internal/personal/observe.go:87-110`）；回合状态 `running → completed/failed/cancelled`（`internal/personal/runner_finish.go:97-107,121`）；`sessions.status` 由 `markRunning` 维护（`internal/personal/runner.go:651-665`）。
- **未完成回合的条目不参与历史回放**：`internal/personal/context.go:105-112,158-160`（`if !done[e.TurnID] { continue }`），compaction 只在 completed turn 采纳（`:140`）；`lastreply.go:64-67` 用 `SessionRunning` 判"未正常结束"。
- 读接口齐备，且能增量 tail：`EntryFilter{SinceSeq,TurnID,Types,Limit}`、`EventFilter{SessionID,TurnID,Kinds,Since,Limit}`（`store/types.go:132-168`）、`LatestEntry`（`entries.go:115`）；**没有 watch/subscribe/notify 类接口**。
- `suga export` 是全量快照 jsonl（可按时间合并 entries + events，支持 `--redact`、`--with-outputs`，`internal/cli/export.go:24-32,78-179`），适合外部一次性拉取，无 tail/follow。

### 写入者仲裁（三层锁）

- 注释本身就把三层写清了：进程内 `sessionGate`（Run / Compact / 空闲整理互斥）、跨进程文件锁 `process.SessionLock`（chat 生命周期级持有）、持久 `store.SessionRunning`（`internal/personal/gate.go:10-26`）；调用顺序是"先取闸门再取文件锁"（`:25-26`）。
- 具体行为：chat 启动即 `HoldSessionLock`（`internal/personal/gate.go:185-219`，`internal/chat/chat.go:165-171`）；第二个 `chat --resume A` 立即 `ErrSessionLocked`（`gate.go:188-200`）；`run --resume A` 等锁 30s 后失败（`internal/personal/runner.go:437-441,448-455`）；ACP 的 `session/load` 只读不锁、`session/prompt` 才 `Run`（`internal/acpserver/server.go:215,242`），所以同样抢锁。
- 锁只在 `LockRoot` 非空时启用（`internal/wiring/runtime.go:363,442`）；子 agent runner 不带锁（`internal/personal/recovery.go:31-34`）。
- stale 恢复靠"持锁即证明"（`recovery.go:21-38,70-133`）；store 无乐观锁：`UpdateSessionFields`/`UpdateTurn` 盲写（`store/sessions.go:154-164`、`store/turns.go:88`），只有 meta 是原子 patch（`sessions.go:196-260`）。

### 外部输入通道（`suga send`）

- chat 在 `SUGA_HOME/run` 登记进程并监听 socket（`internal/personal/process/process.go:97-127`），文件权限 0700/0600，路径长度有上限（`validateSocketPath`，超限则不登记，见 `docs/usage/gofer-personal-agent.md:56`）。
- 协议：**一条连接一个 JSON 对象**（`json.NewDecoder`，`process.go:209`），`Request{type,session_id,text,from,wait}`（`process.go:37-45`）、`Response{ok,queued,error}`（`:47-51`），`--wait` 两阶段终局应答（`:238-253`）。
- chat 侧：入 `deliveries`（缓冲 8）→ `busy || 队列非空` 即 `queued=true` → 标 `injected=true`（`internal/chat/chat_input.go:167-186`，`:172`）→ 加 `[来自 <label>]` 前缀（`:209-214`）→ **只在提示符处消费**（`internal/chat/chat.go:442-449,485-492`）后当普通消息开新回合。
- **即：socket 投递一律是 follow-up，回合进行中只排队，不会插进当前回合**；`suga send` 目前只有 `--session/--text/--source/--wait` 四个选项（`internal/cli/send.go:89-95`），没有 `--steer`。
- Gofer 就是用这条通道把消息送进运行中的终端会话（`deliver_command: [send,--session,{{session_id}}]` + `deliver_stdin`，`docs/usage/gofer-personal-agent.md:54`），并把 web 回复标成注入输入（`--injected`，`:255-256`）。

### 回合中插话（steering）骨架已具备

- 内核完整：`steerInbox`（容量 8、push/take/close/returned）、`Runner.Steer`、`openSteerInbox`、`takeSteers`（落 `EntryUser` + `suga.steer/suga.source/suga.steer_id` 元数据、发 `steer_delivered` 事件）、`returnSteers`（cancelled/failed 回合把已投递的也交回输入行，`internal/personal/steer.go:22-44,74-180,233-266,299-314`）。
- 注释里已把 `suga send --steer`（来源 `send:<from>`）与键盘插话（`keyboard`）当作既定输入源（`steer.go:30-32,46-56,168-180`），但**触发端还没接**：`Request` 无 mode 字段，chat 输入层标注未启用（`internal/chat/chat_input.go:289-294`）。设计已 approved，排在第四批小项之后（`docs/design/2026-10-14-steering-design.md:171-174`，`docs/plans/2026-10-14-steering-plan.md:4`）。

### 流式输出：四条消费者，没有总线

每个消费者由调用方各传一个 `Emit`（`internal/personal/runner.go:537-546`）：

- 终端 chat：`internal/chat/chat_turn.go:107-127` → `render.Handle`
- `run` 文本：`internal/cli/run.go:225-238`（`output.LiveText`）
- `run --output-format stream-json`：`internal/cli/run.go:216-241`、`internal/output/streamjson.go:18-64`
- ACP：`internal/acpserver/server.go:225-241`（`session/update`）
- 旁路：观测落库 `observe.go:87-110`、transcript JSONL（`runner.go:456-464`、`runner_finish.go:22-48`）

模型 delta（思考/正文增量）与渲染态 spinner **不落库**，只在这四条管道里。

### 只读页面先例（`suga trace --web`）

- `internal/cli/trace.go:69-97`：`store.Open`（**可写句柄**，会跑迁移）→ `BuildTraceView` → `ServeTraceView`，`load` 闭包每次请求重建视图，然后 `openBrowser`。
- `internal/personal/traceweb.go:290-299`：`/api/session` 每请求重建（注释"Loaded per request, so a reload shows a session still running"），`:268-276` 只绑回环，`BuildTraceView` 一次拉全量 events + entries（`:90-102`），单条文本 64KB 截断（`:23,171-180`）。
- 前端 `traceweb.html:464` 只在加载时 `fetch("api/session")`：**没有 setInterval / EventSource / WebSocket / SSE**，"刷新即最新"成立，实时性靠手动刷新。
- 形态可直接抄：内嵌单 HTML（`go:embed`）、原生 JS、无构建步骤、只绑回环、Ctrl-C 退出。

### hooks 与 Gofer 侧先例

- 六类 hook：SessionStart、UserPromptSubmit、PostToolUse、Stop、SessionEnd + suga 特有的 Interrupt（`internal/hooks/hooks.go:1-11`）；payload 带 `session_id/cwd/transcript_path/source/prompt/injected/last_assistant_message/stop_hook_active/tool_*`（`:30-44`）。
- Stop 是"外部系统感知回合结束"的唯一现成机制（后台 Stop、Interrupt 顺序见 `hooks.go:64-69,84,163-176,285-300`）；`Response.Notice` 的注释举例就是"**会话已被别处接管**"（`hooks.go:52-54`）。
- `transcript_path` 是每会话一份 append 的 JSONL 快照（`internal/personal/transcript.go:24-51`），供外部 hook 命令读最新消息。
- ACP 侧提问/审批的现成回传路径：`session/request_permission` ↔ `gofer job interactions/answer`（`docs/usage/gofer-personal-agent.md:184-193`），且与 chat 共用同一个审批器。
- 历史上"接管"的语义边界是"**仅限原进程已退出**"（`suag-ifpp.6` 验收记录），源码里没有 takeover/adopt 会话概念。

## 设计要点

### 1. 单写者

- 复用三层锁，**web 的每条写路径都必须经 Runner**（`personal.Run` / `Runner.Steer`），不得自己 `AppendEntries` / `UpdateSessionFields` / `UpdateTurn`。
- 把"谁在跑这个会话"变成两端都能看到的状态：`sessions.status=running` + 进程登记 `Record{state: busy/idle}`（`internal/personal/process/process.go:20-35,146-155`）+ `suga ps`（`internal/cli/ps.go:21-34`）已经够用；web 页面显示"回合由 <短名> 在跑"即可解释"为什么我这里只能排队"。
- 若选路线 B（同进程两前端），单写者退化为**进程内输入所有权**：终端 stdin、web 请求、socket 投递、ACP 都是输入源，同一时刻只有一个回合在跑（`sessionGate` 已保证），剩下的是"输入与提问归哪个前端"的仲裁。

### 2. 读到最新

分两档，先明确能做到哪一档：

| 内容 | 现在能否读到最新 | 依据 |
|---|---|---|
| 用户消息、assistant 消息、工具调用与结果 | 能（逐条落库） | `runtime.go:113-121,583,743-745` |
| 审批/拒绝、steering、分段、压缩等事件 | 能（实时 best-effort） | `observe.go:87-110` |
| 回合与会话状态 | 能 | `runner_finish.go:97-107`、`runner.go:651-665` |
| 思考与正文的逐字 delta、spinner | **不能**（不落库，需要新增订阅通道） | `chat_turn.go:107-127` 等四条 Emit 管道 |

- 第一档足够"刷新即最新"，做法照抄 `trace --web`，但两处要改：**用只读句柄**（现在 `trace.go` 用的是会跑迁移的可写句柄）、**增量拉取**（`SinceSeq` / `Since`）而不是每次全量。
- 第二档需要会话级 fan-out：在 `RunRequest.Emit` 之外暴露一个可订阅的会话通道（SSE 最贴合已有单页形态），chat / web / ACP 都从它取。这是本项第一块要新写的代码。
- web 展示必须按 turn status 过滤：未完成或整轮失败的回合条目不参与历史回放（`context.go:158-160`），否则页面会显示模型自己看不到的内容。

### 3. 输入与历史一致性

- 任何来源的输入都要落成 `EntryUser` + 元数据：steering 已经是这个形状（`steer.go:198-202`），socket 投递现在标 `injected=true` 并加 `[来自 <label>]` 前缀（`chat_input.go:172,209-214`）。
- 建议给 `Request` 加 `mode`（`followup`（默认）/ `steer`），与 steering 设计一致（`docs/design/2026-10-14-steering-design.md:171-174`）：web 队列消息走默认，web 的"立刻插话"走 steer。这样"web 也能在回合中输入"才成立——否则只能等本轮结束。
- 归属要可追溯：`source`（`cli` / `web` / `gofer`）已能显示与记录，web 输入应在事件里带 `source=web`，供 trace 与后续统计区分。

### 4. 审批与提问的所有权（真正的硬骨头）

- 现状是"终端问、终端答"：写工具审批（`internal/chat/chat_approval.go`）、`AskUserQuestion`（`internal/chat/ask.go`）、记忆写入确认、空闲整理询问都在终端；审批是同步等人，**终端没人看时回合会卡在这里**。
- 同会话两前端必须定义：谁答（建议**先到先得**：任一端作答后另一端把该行标记为"已由 web/终端作答"）、以及"待答项"如何被另一端看到。
- 可行做法：把待答项提升为**会话级事件**（trace 已有 `approval` 与 `approval_wait_ms` 的口径），任一端轮询/订阅后作答，先到先得；终端渲染器只负责显示状态。参照 ACP：它已经把权限请求交给客户端，且与 chat 共用同一审批器。
- 这条也决定了"web 打开时终端还能不能安全退出"：如果审批可能来自 web，终端 Ctrl-D 退出前要处理"无人应答"的兜底（拒绝并说明，或把待答项转交 web）。

### 5. 中止与回合所有权

- 现在中止是本地的：终端 Esc / Ctrl-C 只中止本轮（chat 继续），并且有已知问题"工具调用进行中按 Esc / Ctrl+C 无反应"（`suga-nrf2`）。web 需要"谁可以中止"的规则：建议只允许**当前有输入权的前端**中止，或在会话级事件里记一条 `abort_requested`，由回合执行者执行取消。
- 回合取消后已投递的 steering 会退回输入行（`steer.go:268-280`），web 侧要能显示这批"没送出去的消息"，否则用户会以为已送达。

### 6. 与 `trace --web` 的关系

- 二者共用服务形态与前端资产，但一个是只读观测、一个是交互入口。两种做法都可接受：
  1. **两个页面共用一个服务**：`suga web` 里同时有会话页与时间线页（页面内切换），实现上复用 `ServeTraceView` 的 `/api/session`；
  2. 各自独立：`trace --web` 保持纯只读，`suga web` 另写页面，共用 store 读层与 HTML 构建约定。
- 建议先 1（一个服务、两个 tab），因为观测与交互在实战中是同一件事的两面。

### 7. 安全

- 先例是"只绑回环 + 0700/0600 文件权限 + 只允许本机用户"（`traceweb.go:268-276`、`process.go:97-127`）。web 多了一个输入面，建议在回环之上加**一次性 token**（启动时打印带 token 的 URL，页面后续请求带同一 token）：浏览器里的其它页面、本机其它进程都能往这个端口发 POST。
- socket 投递不校验 `Record.Host`（`process.go:291-302`），共享 SUGA_HOME（多机/容器同挂载）时的行为未验证，web 要做鉴权时别依赖这条。

## 落地顺序建议

1. **第一期：只读观测 + socket 输入**。web 不跑回合，输入走 `suga send` 语义；单写者零新增；读到最新靠轮询 events/entries/status；审批留在终端。可以先把它当作"会话观测页 + 输入框"。
2. **第二期（本项目标形态）：`chat --web` 同进程两前端**。抽象输入源、会话级提问/审批事件（先到先得）、加 SSE 让页面有流式；顺手把 `--steer` 接到 socket 即得"回合中也能插话"。
3. **第三期：web 独占跑回合 + 接管**。`suga web --session <id>` 经 Runner 跑回合（flock 保证与终端互斥），并在原进程退出后允许接管（沿用 suag-ifpp.6 的"仅限原进程已退出"语义）。

**代价要提前说清**：第一期与"Gofer Web + `suga-acp`"能力重叠（浏览器输入、审批从 web 答，Gofer 侧已通），suga 自带页面得靠"不装 Gofer 也能用""与 trace 观测同页""本地 steering 更快"这些增量立足。

## 未验证与风险

- 本文全部为静态核对（基线 `7046c5d`），**没有运行时验证**：socket 投递与 web 并发的真实表现、两前端同会话的实际时序都未实测。
- Windows：socket 路径长度上限（Windows 100 code units，超限则不登记，`docs/usage/gofer-personal-agent.md:56`）；跨进程锁在 Windows 走 `LockFileEx`（`internal/personal/process/lock_windows.go:18-20`）——真机行为未验证。
- SQLITE_BUSY：并发写同一库有过前科（`suag-5o5g`），web 高频轮询读会放大写等待（`busy_timeout` 15s）。
- 共享 SUGA_HOME（多机/多容器）下 `Record.Host` 未参与校验（`process.go:291-302`）。
- 内存态与 store 的差异：chat 每回合重新 `loadHistory`（`internal/personal/runner.go:481`、`context.go:104-173`），不做内存缓存，所以 web 读 store 与 chat 看到的差别只在"未完成回合的过滤"上；若将来引入会话级缓存，这条要重评。

## 参考

- 设计/计划：`docs/design/2026-10-14-steering-design.md`（steer/follow-up、会话收件箱、`suga send --steer`）、`docs/plans/2026-10-14-steering-plan.md`、`docs/design/2026-10-13-trace-web-design.md`（`trace --web`）、`docs/design/2026-10-13-background-tasks-design.md`（三种入口的生命周期差异）。
- 接入合同：`docs/usage/gofer-personal-agent.md:50-56`（socket 投递、来源前缀、注入标记、会话锁）、`:166-167`（trace --web / export）、`:184-193`（ACP 审批）、`:250-256`（web 回复与 `--injected`）。
- issue：`suga-lrqy`（本项）、`suag-mbwu`（steering 与中止）、`suga-nrf2`（回合中 Ctrl-C 无效）、`suga-hko7`（会话间互发）、`suga-8pug`（follow-up 键）、`suga-9os5`（`!` 本地命令）、`suga-o26e`（取消/失败回合的任务通知）、`suga-wy40`（短名）、`suag-ifpp.6`（接管仅限原进程已退出）、`suag-nmed`（trace --web）、`suag-5o5g`（SQLITE_BUSY）、`suga-uh0j`（socket 路径长度）。
- 历史：旧自托管 Web 控制面已删除并归档于 `docs/archive/legacy-host/`，不再作为合同（`docs/design/2026-10-06-personal-agent-redesign-design.md:13-14,21-25`）。
