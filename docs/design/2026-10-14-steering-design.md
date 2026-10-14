<!-- template_id: design; template_version: 1.1.1 -->
# 运行中插入消息（steering）与只中止当前回合 设计

> 状态：Draft 0.1 / 待人工计划批准

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-14 | Claude（监督会话） | 初稿 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

关联：issue suag-mbwu，plan-20261014-102002-6f9b4d19（D 项）。相关：suga-nkye（崩溃后会话停在 running）、suga-wy40（会话短名称）、suga-az4l（步数上限收尾）。参考：pi coding-agent 的 steering / follow-up / abort（`tmp/refer-src/pi-mono/packages/coding-agent/docs/`：`how-pi-works.md`、`usage.md`、`rpc-commands.md`、`settings.md`）。

### 思考模式与范围冻结

- thinking_mode：RIGOROUS。
- core_objective：用户在模型干活时能（1）插一句话纠偏，模型在下一次请求前看到；（2）只停掉这一轮，chat 不退出。
- allowed_scope：`pkg/agentcore`（回合循环的续跑点）、`internal/personal`（会话收件箱、持久化、事件）、`internal/chat`（回合中的输入行、按键）、`internal/personal/process` 与 `internal/cli/send.go`（`--steer`）、`internal/output/streamjson.go`、trace。
- non_goals：ACP 的 steering（见 D8，延后）；会话短名称与会话间互发（suga-wy40）；排队消息的跨进程持久化；gofer 侧改动。
- expansion_policy：DEFER_OR_REQUEST。
- review budget：一轮合并轴独立评审（低暴露度：个人工具、未投产、可回滚；SR1409）。
- 停止条件：评审无 CORE_BLOCKING 且待确认事项经用户答复后，进入计划。

## 背景与目标

用户 2026-10-13 提出“运行中插入信息怎么做”。现状是：模型一轮可以连续调用几十次工具，期间用户发现方向不对，只能等它跑完，或者按 Ctrl-C 把整个 chat 杀掉再续接。

用户可见目标：

1. **插话（steer）**：回合进行中，输入行保持可用。键入一句话回车，模型在“当前这次回复及其工具调用完成后、下一次模型请求前”看到它，并据此调整，本轮不中断。
2. **只中止本轮**：回合进行中按 Esc 或一次 Ctrl-C，只取消当前回合（会话保留、chat 不退出），回到提示符；连按两次 Ctrl-C 才退出 chat，与提示符处现有行为一致。
3. **send 也能插话**：`suga send --steer` 把消息插进正在进行的回合；默认行为（回合结束后作为新回合）不变。
4. 插话可观测：chat 显示、trace 记录、stream-json 有对应的行；续接、`suga trace` 都能看出这句话是在回合中插入的。

## 名词

- **回合（turn）**：一次 `Runner.Run`，包含多次模型请求和工具调用，直到模型给出最终回复或出错/取消。
- **步（step）**：回合内的一次模型请求（及其工具调用），受 `max_steps` 约束。
- **插话（steer）**：回合进行中提交、在下一次模型请求前进入上下文的用户消息。
- **追加（follow-up）**：回合进行中提交、等本轮结束后作为下一回合的消息（现有 `suga send` 默认行为）。
- **会话收件箱（inbox）**：本设计新增，Runner 内按会话保存待插入的插话。

## 范围与非目标

范围：

- agentcore：回合在模型给出最终回复时，若收件箱非空，则续跑一步而不是结束。
- personal：会话收件箱、插话的持久化与事件、回合关闭时的竞争处理。
- chat：回合进行中保持行编辑；Enter 插话；Esc / 单次 Ctrl-C 中止本轮；排队插话的显示与撤回。
- send：`--steer`；协议加字段，旧客户端保持原行为。
- 观测：trace 事件、stream-json 行、chat 渲染。

非目标：

- ACP 的 steering（D8 延后）。`session/cancel` 已能只中止当前 prompt，ACP 现有行为不变。
- 会话短名称、会话间互发（suga-wy40 单独做；本设计只保证 send 协议的新字段能被它复用）。
- 进程退出后保留排队消息；插话的编辑历史；多行插话的特殊编辑器。
- 子 agent 的插话（插话只进入父回合；子 agent 的结果照常回到父回合）。

## 已确认事实与规范

以下均已在代码中核对（main，v0.20.0）。

**chat 输入**

- 只有在读一行时才进入 raw 模式；回合进行中 `SetBusy(true)` 置 `turnActive` 并强制回到 cooked 模式（`internal/chat/chat_input.go:212-220、292-294`）。回合中没有协程读 stdin，键入内容由终端缓冲，回合结束后才出现在下一次提示符里。
- 回合中的 Ctrl-C 是 SIGINT：`cmd/suga/main.go:36` 的 `signal.NotifyContext` 取消进程级 ctx；chat 在回合返回后 `if ctx.Err() != nil { return ctx.Err() }` 直接退出（`internal/chat/chat.go:379-381`），退出码 130。chat 没有回合级 ctx。
- 提示符处的 Ctrl-C 由 lineedit 读到 0x03 返回 `ErrInterrupt`；3 秒内第二次才退出（`chat.go:261-270`，`exitConfirmWindow`）。
- 已有一条“只结束本轮”的路径：审批提问时 Ctrl-C/Ctrl-D 返回 `errAskCancelled`（包装 `context.Canceled`），经 `abortingToolPolicy` 让回合失败，chat 显示“已取消”并继续（`internal/chat/ask.go:14`、`internal/personal/denial.go:116-125`、`chat.go:382-384`）。
- 渲染器的所有输出都经过 input source；有终端时写到 `lineedit.Terminal.Write`，它负责在输入行上方输出并重绘输入行（`chat_input.go:266-271`）。

**send 投递**

- `suga send` 经会话 socket 投递；chat 侧 `DeliverWait` 在忙时返回 `queued=true`，队列上限 8（`chat_input.go:129-148`，`internal/personal/process/process.go:18`）。投递只在两回合之间被 `nextChatInput` 消费（`chat.go:499-506`），作为新的注入回合运行。`--wait` 等到消息开始成为回合；退出时未消费的消息被丢弃，`--wait` 的发送方得到退出码 3。

**回合中注入消息的现成机制（后台任务通知）**

- agentcore 每次模型请求前都调用 `ContextBuilder.BuildMessages`（`pkg/agentcore/runtime.go:150`）。personal 的实现先 `takeTaskNotices`，把通知作为 `role=user` 消息（metadata `suga.task_notice`）插到当前位置，并以 `EntryUser` 落库到当前 turn（`internal/personal/context.go:225-229`，`internal/personal/tasks.go:54-87`），`compose` 原位回放（`context.go:279-290`）。
- 已完成回合的条目在后续回合按顺序回放；被取消回合的条目不回放（`context.go:73-90`）。
- agentcore 的 `Control.RequestWrapUp` 在下一轮循环开头生效（`runtime.go:136-139`）；模型给出无工具调用的回复即结束回合，目前没有“还有待处理输入就继续”的机制。

**取消的记录**

- `Runner.finish` 以 `ctx.Err() != nil || errors.Is(runErr, context.Canceled)` 判定取消，turn 记为 `cancelled`，会话写回 `open`（`internal/personal/runner.go:746-808`）。

**ACP**

- 同一会话 prompt 进行中再发 `session/prompt` 返回 InvalidParams；`session/cancel` 只取消当前 prompt，返回 `StopReasonCancelled`（`internal/acpserver/server.go:197-251、318-330`）。

**观测**

- stream-json 由 `internal/output/streamjson.go` 把 Runner 事件映射为行，未知种类丢弃；trace 事件种类见 `internal/personal/store/types.go:45-73`。

**适用规范**：SR1207（大型功能流程）、SR1204（范围确认）、SR1402.1（真实终端验证与披露）、SR1433（委派实施的反向验证）、GR104（单文件 ≤1000 行，`chat_input.go` 445 行、`lineedit/terminal.go` 967 行，新增逻辑需另起文件）。

## 总体方案

一条主线：**插话进入 Runner 的会话收件箱 → 下一次 `BuildMessages` 时取出、落库、插入上下文 → 若模型本来要结束，则因收件箱非空续跑一步**。chat 键盘、`suga send --steer` 都只是往收件箱里放消息的入口；中止本轮则是给 chat 加一个回合级 ctx，并让回合中的 Ctrl-C/Esc 成为按键而不是 SIGINT。

这样复用了后台任务通知已经验证过的注入路径（落库、原位回放、续接一致），新增的核心只有两处：收件箱（含关闭竞争）和 agentcore 的续跑点。

## 架构

```text
 键盘 Enter ─┐                         ┌─ Runner.inbox[session] ─┐
             ├─> Runner.Steer(sid,msg) ┤  (互斥 + closing 标志)  │
 send --steer┘   accepted? ──否──> 退回调用方作为 follow-up      │
                                                                   ▼
 agentcore 回合循环: buildPrompt → BuildMessages(取出插话、落库、发事件) → 模型
                         ▲                                           │
                         └── 最终回复且 PendingInput()==true 时续跑 ──┘
```

### agentcore：续跑点

- `agentcore.Config` 新增可选 `PendingInput func(ctx) bool`（不依赖 internal）。模型给出无工具调用的最终回复时，若 `PendingInput` 返回 true，且未处于收尾（wrap-up）状态、未超过 `max_steps`，则把这次回复作为普通 assistant 消息保留、进入下一步，而不是结束回合。
- 续跑的一步照常计入 `max_steps`；处于收尾状态时不续跑（收尾优先，剩余插话按 follow-up 处理，见 D5）。

### personal：会话收件箱

- `Runner.Steer(sessionID, Steer{Text, Source}) (accepted bool, err error)`：仅当本进程正在为该会话运行回合、且回合未进入关闭阶段时接受，返回 true；否则返回 false，由调用方决定退回（键盘输入放回输入行，send 转为 follow-up）。
- 收件箱随回合创建、随回合结束清理；`Runner.finish` 开始时先在锁内置 `closing`，再取出剩余插话交还调用方（`RunResult.UnconsumedSteers`），保证“已接受的插话要么进了上下文，要么明确退回”，不会静默丢失。
- `BuildMessages` 中 `takeSteers` 与 `takeTaskNotices` 并列：每条插话作为 `role=user` 消息，metadata `suga.steer: true`、`suga.source: <keyboard|send:<from>>`，以 `EntryUser` 落库到当前 turn，记 trace 事件，并通过 turn 的 Emit 发出 `steer` 事件。
- 文本格式：键盘插话原样；send 插话沿用现有“[来自 <from>] ”标注规则。

### chat：回合中的输入与中止

- 回合进行中保持 raw 模式与一个活动的 lineedit 读取（提示符改为 `↳ ` 等区别于 `❯ ` 的样式，具体见 D2）。模型输出照常经 `Terminal.Write` 写在输入行上方。
- Enter：调用 `Runner.Steer`；接受则在输出区显示一行淡色“[插话] <文本>（将在下一步送达）”，送达（收到 `steer` 事件）时改为正常显示；不接受（回合恰好结束）则把文本作为下一回合的输入直接运行（等同 follow-up）。
- Esc 或单次 Ctrl-C：取消回合级 ctx（`context.WithCancel` 派生自进程 ctx），显示“已中断本轮”，回合记为 cancelled；未送达的插话放回输入行，便于修改后重发。3 秒内第二次 Ctrl-C 退出 chat。
- raw 模式下 Ctrl-C 不再产生 SIGINT，所以工具子进程不会再收到终端的 SIGINT；它们随回合 ctx 取消被进程树清理（与现有超时路径相同）。SIGTERM 仍终止整个 chat。
- 审批提问（Ask）进行中：输入行暂停，已键入但未提交的插话文本暂存，问题回答后恢复。
- 现有回合中 cooked 模式、预输入缓冲的行为被取代；chat 的非终端模式（stdin 不是 tty）不变。

### send：`--steer`

- `process.Request` 加字段 `Mode`（`"follow_up"` 默认 / `"steer"`）；旧客户端不带该字段即为 follow-up，行为不变。
- `--steer` 时 chat 侧先尝试 `Runner.Steer`：接受则响应 `steered`（`--wait` 等到 `steer` 事件即送达后返回）；会话空闲或回合正在结束则按 follow-up 排队，响应 `queued`。退出码不变。

### 观测

- trace：新增事件种类 `steer`（attrs：source、chars、step），也可在 `suga trace` 中把带 `suga.steer` 的 user 条目标为“插话”。
- stream-json：新增 `{"type":"steer","session_id","run_id","text","source"}` 行，以及取消时现有的 `turn_end`/`result` 状态不变。
- chat 回合结束的状态行显示本轮插话次数（有时）。

## 关键流程

### 插话在工具调用之间送达

```text
用户 Enter "别改 config，只改 README"
  chat → Runner.Steer(sid) → inbox=[msg], accepted
  ...当前工具调用完成...
  agentcore → BuildMessages → takeSteers: 落库 EntryUser{suga.steer}，事件 steer
  模型请求含该消息 → 模型调整方向
  chat 收到 steer 事件 → 显示“[插话] 别改 config，只改 README”
```

### 模型正好给出最终回复

```text
模型返回无工具调用的回复 → agentcore 问 PendingInput() → true
  → 保留该回复，进入下一步 → BuildMessages 取出插话 → 模型针对插话再回复
若 PendingInput()==false → 回合结束 → finish 置 closing，收件箱为空
若插话在 closing 之后到达 → Steer 返回 accepted=false → chat 将其作为下一回合运行
```

### 只中止本轮

```text
回合中按 Esc → 取消回合 ctx → 正在进行的模型请求/工具调用随 ctx 结束
  → finish: turn=cancelled，会话 open，UnconsumedSteers 交还 chat
  → chat 显示“已中断本轮”，把未送达插话放回输入行 → 回到提示符
```

## 安全、数据、运维与回滚

- 安全：插话与普通用户输入同权，不绕过审批与只读模式；`--steer` 只能投递到同用户可访问的会话 socket，权限边界与现有 send 相同（socket 目录 0700 / Windows ACL）。
- 数据：插话以 `EntryUser` 落库，新增 metadata 键，不改 schema；被取消回合的插话随该回合不回放（与现有一致）。
- 兼容：send 协议新字段可缺省；stream-json 新增行类型；suga 自身对未知种类是丢弃的，gofer 对未知行的处理未核实，实施前确认（必要时同步 gofer 会话）。
- 运维：新增配置 `chat.steering`（默认开，关掉时回合中恢复现行 cooked 行为，作为回退开关）。
- 回滚：各阶段独立提交；关闭 `chat.steering` 即回到现行 chat 行为，send 不带 `--steer` 时行为不变。
- 平台风险：Windows 控制台在 raw 模式下 Ctrl-C 的读取方式需在真机确认（主机无 pty，只能人工验证，SR1402.1 记为 UNVERIFIED_REAL_ENV 直至确认）。

## 决策

- **D1 注入路径**：复用后台任务通知的路径（`BuildMessages` 中取出、`EntryUser` 落库、原位回放），不新建消息通道。理由：续接、回放、trace web 已有支持。
- **D2 键位**：回合中 Enter = 插话；Esc 或单次 Ctrl-C = 中止本轮；3 秒内两次 Ctrl-C = 退出 chat；Ctrl-D 在回合中无效（避免误退）。回合中的提示符用 `↳ `（待确认事项 Q1）。
- **D3 模型已给出最终回复时**：同一回合续跑一步处理插话（与 pi 一致），而不是结束后另起回合。理由：避免中间触发 Stop hook 与回合结束通知，`send --steer --wait` 的语义也更简单。
- **D4 没送达的插话不丢**：回合关闭阶段到达的插话退回调用方（键盘：作为下一回合；send：按 follow-up 排队）；中止本轮时未送达的键盘插话放回输入行，send 来的插话保持排队成为下一回合。
- **D5 收尾优先**：处于 wrap-up（步数/token/时长上限）时不续跑，不取插话；剩余插话按 D4 退回。
- **D6 每步全部送达**：一次 `BuildMessages` 取出收件箱中全部插话，各自一条 user 消息（pi 的 `steeringMode=all`）；不提供 one-at-a-time 配置（无已验证需求，SR1405）。
- **D7 follow-up 键**：chat 回合中不另设 follow-up 键；需要“等本轮结束再说”的用户等回合结束后输入即可。send 的默认行为就是 follow-up。（待确认事项 Q2）
- **D8 ACP 延后**：目前没有已知的 ACP 客户端需要在 prompt 进行中再发消息（未逐一核实 Zed 与 gofer 的 suga-acp 行为）；在出现明确需要前不实现 ACP steering。ACP 的 InvalidParams 行为不变。

## 待确认事项

- **Q1** 回合中的输入提示符样式：`↳ `、`❯ (插话) ` 还是保持 `❯ ` 只换颜色？建议 `↳ `，一眼区分“现在输入的是插话”。
- **Q2** 是否需要 chat 回合中的 follow-up 键（如 Alt+Enter）？建议先不做（D7），有需要再加；lineedit 目前不识别 Alt+Enter。
- **Q3** Esc 中止是否需要确认？建议不需要：中止只影响本轮，已完成的工具效果不回滚，会话可继续；误按的代价低。
- **Q4** 实施分阶段：建议 S1 agentcore 续跑点 + personal 收件箱/落库/事件（无 UI，单测与 stream-json 可验证）；S2 chat 回合中的输入、插话与中止（ptysmoke + 真实终端人工确认）；S3 `send --steer` 与协议字段。是否同意按此拆分并在计划中各自验收？

## 结论与人工计划 Gate

方案以“会话收件箱 + agentcore 续跑点”为核心，chat 键盘与 `send --steer` 作为入口，ACP 延后；主要风险在 chat 回合中改为 raw 模式后的终端行为（含 Windows），通过 `chat.steering` 开关回退。

本稿为 Draft 0.1，下一步：一轮独立评审 → 用户答复 Q1–Q4 并批准 → 起草实施计划。批准设计不授权实施。
