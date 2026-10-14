<!-- template_id: design; template_version: 1.1.1 -->
# 运行中插入消息（steering）与只中止当前回合 设计

> 状态：Draft 0.2 / 待人工计划批准

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-14 | Claude（监督会话） | 初稿 |
| 0.2 | 2026-10-14 | Claude（监督会话） | 按独立评审（codex job 20261014-114209-25b62278，结论 BLOCKED）修订：取消时交还本回合全部插话（发现 1）；续跑点的精确条件（发现 2）；chat 输入层按模式分流与回合期间消费者（发现 3）；插话在段交接/压缩中的语义（发现 4）；收件箱容量、`--steer` 回退与 `--wait` 完成点、验收口径（发现 5） |

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

### agentcore：续跑点（0.2 补全状态机，评审发现 2）

- `agentcore.Config` 新增可选 `PendingInput func(ctx) bool`（不依赖 internal）。
- 检查点只有一个：模型给出无工具调用的最终回复、`persistFinalAnswer` 成功落库之后。续跑条件必须同时满足：
  `!wrappingUp && ctrl.WrapUpReason == "" && (maxSteps == 0 || state.Step < maxSteps) && PendingInput(ctx)`。
  满足时把这次回复保留为普通 assistant 消息、进入下一步（下一步的 `BuildMessages` 取出插话）；不满足时立即返回已落库的最终回复，收件箱里的插话按 D4 退回。
- 因此：最后一步（`state.Step == maxSteps`）、已处于收尾、或本次模型/Hook 刚请求了收尾时，都不续跑，不会多发模型请求，也不会让插话进入 `ErrWrapUpIgnored` 路径。续跑的一步照常计入 `max_steps`。
- 测试必须覆盖：最后一步有插话；Hook 在本次请求设置 `RequestWrapUp`；`MaxStepsWrapUp` 已启用；以及正常续跑。

### personal：会话收件箱（0.2 改为“取出即登记、取消即退回”，评审发现 1、5）

- `Runner.Steer(sessionID, Steer{Text, Source}) (accepted bool, err error)`：仅当本进程正在为该会话运行回合、回合未进入关闭阶段、且收件箱未满时接受。容量与 send 队列一致（8 条）；满了返回 `ErrSteerQueueFull`，键盘入口提示“插话过多，请等本步结束”，send 入口沿用现有的 `queue_full` 响应（`internal/personal/process/process.go:232`）。其余情况返回 false，由调用方退回（键盘：作为下一回合；send：转 follow-up 排队）。
- 每个回合维护两张表：`inbox`（已接受、未取出）和 `taken`（本回合已取出并落库）。`BuildMessages` 中 `takeSteers` 把 inbox 中的全部插话移入 `taken`，作为 `role=user` 消息（metadata `suga.steer: true`、`suga.source: <keyboard|send:<from>>`）以 `EntryUser` 落库到当前 turn，并发 `steer` 事件、记 trace。
- `Runner.finish` 在锁内先置 `closing`，再决定交还什么（`RunResult.UnconsumedSteers`）：
  - 回合正常结束（completed / wrap-up）：只交还 `inbox` 中未取出的插话；`taken` 已经进入了该回合，随回合历史回放。
  - 回合被取消或失败：交还 `taken + inbox` 全部插话。理由：被取消/失败回合的条目在后续回合不回放（`context.go:73-90`），即使模型在本回合里看到过，它们对之后的对话也等于不存在；全部交还才满足“已接受的插话要么进入（被保留的）上下文、要么明确退回”。这也覆盖了评审指出的竞态——取消发生在 `BuildMessages` 之后、模型请求之前。
- 调用方处理交还的插话：chat 把键盘插话放回输入行（多条用换行连接），send 来的转为 follow-up 排队；trace 记 `steer` 事件的 `returned` 状态。
- 测试必须覆盖：取消发生在 `BuildMessages` 与模型请求之间（可注入的 provider 阻塞点）；`closing` 之后到达的 `Steer`；收件箱已满。
- 文本格式：键盘插话原样；send 插话沿用现有“[来自 <from>] ”标注规则。

### personal：插话在段交接、压缩与记忆中的语义（0.2 新增，评审发现 4）

- 插话不是回合的原始请求。`recentRequests`（`internal/personal/segment_handoff.go:86-145`）只把回合的原始 prompt 计入“最近的用户请求（原文）”，与现有排除 `suga.injected` 的做法一致；同时在交接包中新增“本回合插话”小节，按时间列出带 `suga.steer` 的消息（标注来源），保证纠偏信息不丢、也不冒充原始任务。
- 压缩/段交接摘要的输入中，插话消息前加固定标签 `[插话]`，让摘要模型能区分。
- 记忆提炼（若读取 user 消息）同样按来源标注；不额外改变记忆审批流程。
- 测试：压缩发生在插话前、插话后两种顺序时，交接包中原始请求与插话各在其位。

### chat：回合中的输入与中止（0.2 重写，评审发现 3）

现有输入源是“一次 `Request` 读一行”，回合进行中主循环同步阻塞在 `runner.Run`，`events`（缓冲 1）没有消费者，首字节 activity 握手会卡住；raw 模式下 Ctrl-C 被 lineedit 转成 `io.EOF`，会被当作退出。因此 S2 先重构输入层，再加插话：

1. **按模式分流的输入事件**。输入源有三种读取模式，同一时刻只有一个活动读取：
   - `prompt`：回合之间读一行（现状）；Enter 产生 `line` 事件，Ctrl-C/Ctrl-D 按现有规则处理。
   - `steer`：回合进行中读一行；Enter 产生 `steer` 事件；Ctrl-C 产生 `abort` 事件（不再是 `io.EOF`），Esc 产生 `abort` 事件，3 秒内第二次 Ctrl-C 产生 `exit` 事件；Ctrl-D 忽略。lineedit 为此新增按键回调（区分 0x03、Esc 单键与 Esc 开头的转义序列，Esc 单键需短超时判定）。
   - `ask`：审批提问（现状）。进入 `ask` 时暂停 `steer` 读取：把已键入未提交的半行存入 `savedLine`（复用现有机制），回答后恢复 `steer` 读取并回填半行。
2. **回合期间的消费者**。chat 在回合开始时启动一个回合输入协程（与 `runner.Run` 并行），它是 `steer`/`abort`/`exit` 事件的唯一消费者：`steer` → `Runner.Steer`；`abort` → 取消回合级 ctx；`exit` → 取消回合并标记退出。回合结束时停止该协程并切回 `prompt` 模式。`steer` 模式下不发 activity 事件（activity 握手只服务于 Stop hook 等待，保持原状）。
3. **`SetBusy/Request/Ask` 的状态转移**：`SetBusy(true)` 由“强制 cooked”改为“切到 `steer` 模式（保持 raw）”；`Request` 切回 `prompt`；`Ask` 在 `steer` 模式下按上面第 1 点暂停与恢复。`chat.steering: false` 时 `SetBusy(true)` 保持现有行为（cooked，Ctrl-C 为 SIGINT），作为回退。
4. **回合级 ctx**：`runner.Run` 改用 `context.WithCancel(ctx)` 派生的回合 ctx；`abort` 只取消它，显示“已中断本轮”，回合记为 cancelled，交还的插话放回输入行。raw 模式下 Ctrl-C 不再产生 SIGINT，所以终端不再把 SIGINT 发给工具子进程；子进程随回合 ctx 取消被进程树清理（与现有超时路径相同）。SIGTERM 仍终止整个 chat。
5. **显示**：`steer` 模式提示符 `↳ `（Q1）；Enter 提交后在输出区显示淡色“[插话] <文本>（等待送达）”，收到 `steer` 事件后改为正常插话样式（与 suga-lebs 的输入行背景一致）；不接受时直接作为下一回合运行。模型输出照常经 `Terminal.Write` 写在输入行上方。
6. **Windows**：suga-nrf2（Windows 上回合中 Ctrl+C 无反应）的根因须在 S2 前定位；S2 在 Windows 上同样走按键路径（控制台 raw 模式下 Ctrl+C 作为输入字符读取），需真机确认，无法自动化部分记为 UNVERIFIED_REAL_ENV。
7. chat 的非终端模式（stdin 不是 tty）不变：不支持插话，Ctrl-C 仍为 SIGINT。

### send：`--steer`

- `process.Request` 加字段 `Mode`（`"follow_up"` 默认 / `"steer"`）；旧客户端不带该字段即为 follow-up，行为不变。
- `--steer` 时 chat 侧先尝试 `Runner.Steer`：接受则响应 `steered`；会话空闲、回合正在结束、或 chat 关闭了 `chat.steering`，则按 follow-up 排队并响应 `queued`（客户端打印“未插入当前回合，已排队”），退出码不变；收件箱已满按现有队列已满处理。
- `--wait` 的完成点（0.2 明确）：`steered` 的消息以 `steer` 事件（已落库并进入模型请求的上下文）为完成点；若之后回合被取消导致该插话被交还并转为 follow-up，则继续等它作为下一回合开始；chat 退出时被丢弃则按现有规则退出码 3。

### 观测

- trace：新增事件种类 `steer`（attrs：source、chars、step、status=`delivered|returned`），`suga trace` 把带 `suga.steer` 的 user 条目标为“插话”。
- stream-json：新增 `{"type":"steer","session_id","run_id","text","source"}` 行（送达时）；取消时现有的 `turn_end`/`result` 状态不变。
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
若 PendingInput()==false 或已到最后一步/处于收尾 → 回合结束 → finish 置 closing，未取出的插话交还调用方
若插话在 closing 之后到达 → Steer 返回 accepted=false → chat 将其作为下一回合运行
```

### 只中止本轮

```text
回合中按 Esc → 取消回合 ctx → 正在进行的模型请求/工具调用随 ctx 结束
  → finish: turn=cancelled，会话 open，UnconsumedSteers = 本回合已取出 + 未取出的全部插话
  → chat 显示“已中断本轮”，把键盘插话放回输入行、send 插话转 follow-up → 回到提示符
```

## 安全、数据、运维与回滚

- 安全：插话与普通用户输入同权，不绕过审批与只读模式；`--steer` 只能投递到同用户可访问的会话 socket，权限边界与现有 send 相同（socket 目录 0700 / Windows ACL）。
- 数据：插话以 `EntryUser` 落库，新增 metadata 键，不改 schema；被取消回合的插话随该回合不回放（与现有一致）。
- 兼容：send 协议新字段可缺省；stream-json 新增行类型；suga 自身对未知种类是丢弃的，gofer 对未知行的处理未核实，实施前确认（必要时同步 gofer 会话）。
- 运维：新增配置 `chat.steering`（默认开，关掉时回合中恢复现行 cooked 行为，作为回退开关；此时 `send --steer` 退化为 follow-up 并响应 `queued`）。
- 回滚：各阶段独立提交；关闭 `chat.steering` 即回到现行 chat 行为，send 不带 `--steer` 时行为不变。
- 平台风险：Windows 控制台在 raw 模式下 Ctrl-C 的读取方式需在真机确认；suga-nrf2（Windows 上回合中 Ctrl+C 无反应）须在 S2 前定位根因。无法自动化的部分按 SR1402.1 记为 UNVERIFIED_REAL_ENV。

## 决策

- **D1 注入路径**：复用后台任务通知的路径（`BuildMessages` 中取出、`EntryUser` 落库、原位回放），不新建消息通道。理由：续接、回放、trace web 已有支持。
- **D2 键位**：回合中 Enter = 插话；Esc 或单次 Ctrl-C = 中止本轮；3 秒内两次 Ctrl-C = 退出 chat；Ctrl-D 在回合中无效（避免误退）。回合中的提示符用 `↳ `（待确认事项 Q1）。
- **D3 模型已给出最终回复时**：同一回合续跑一步处理插话（与 pi 一致），而不是结束后另起回合。理由：避免中间触发 Stop hook 与回合结束通知，`send --steer --wait` 的语义也更简单。
- **D4 插话不丢**：回合关闭阶段到达的插话退回调用方（键盘：作为下一回合；send：按 follow-up 排队）；回合被取消或失败时，本回合已取出与未取出的全部插话都交还（被取消回合的条目不回放），键盘插话放回输入行，send 来的插话转为 follow-up。
- **D5 收尾优先**：处于 wrap-up（步数/token/时长上限）时不续跑，不取插话；剩余插话按 D4 退回。
- **D6 每步全部送达**：一次 `BuildMessages` 取出收件箱中全部插话，各自一条 user 消息（pi 的 `steeringMode=all`）；不提供 one-at-a-time 配置（无已验证需求，SR1405）。
- **D7 follow-up 键**：chat 回合中不另设 follow-up 键；需要“等本轮结束再说”的用户等回合结束后输入即可。send 的默认行为就是 follow-up。（待确认事项 Q2）
- **D9 输入层先重构**：chat 输入源改为按模式（prompt / steer / ask）分流、回合期间由独立协程消费；raw 模式下 Ctrl-C/Esc 在 steer 模式产生 abort 事件而不是 `io.EOF`。理由：现有“一次请求读一行 + activity 握手”的管线无法在回合执行期间产生事件（评审发现 3）。
- **D10 插话不计入原始请求**：段交接的“最近用户请求”只含回合原始 prompt，插话单列为“本回合插话”，压缩输入中带 `[插话]` 标签。
- **D11 收件箱容量 8**：与 send 队列一致，满了明确拒绝而不是静默丢弃。
- **D8 ACP 延后**：目前没有已知的 ACP 客户端需要在 prompt 进行中再发消息（未逐一核实 Zed 与 gofer 的 suga-acp 行为）；在出现明确需要前不实现 ACP steering。ACP 的 InvalidParams 行为不变。

## 待确认事项

- **Q1** 回合中的输入提示符样式：`↳ `、`❯ (插话) ` 还是保持 `❯ ` 只换颜色？建议 `↳ `，一眼区分“现在输入的是插话”。
- **Q2** 是否需要 chat 回合中的 follow-up 键（如 Alt+Enter）？建议先不做（D7），有需要再加；lineedit 目前不识别 Alt+Enter。
- **Q3** Esc 中止是否需要确认？建议不需要：中止只影响本轮，已完成的工具效果不回滚，会话可继续；误按的代价低。
- **Q4** 实施分阶段：建议 S1 agentcore 续跑点 + personal 收件箱/取出登记/交还/段交接语义/事件（无 UI，单测与 stream-json 验收，含取消竞态测试）；S2 chat 输入层按模式分流 + 回合期间消费者 + 插话与中止（Linux pty 冒烟 + 反向验证为必选；Windows 控制台另行真机确认，记 UNVERIFIED_REAL_ENV；前置：定位 suga-nrf2）；S3 `send --steer`、协议字段与 `--wait` 语义。是否同意按此拆分并在计划中各自验收？

## 结论与人工计划 Gate

方案以“会话收件箱（取出登记、取消交还）+ agentcore 续跑点”为核心，chat 键盘（先重构输入层）与 `send --steer` 作为入口，ACP 延后；主要风险在 chat 回合中改为 raw 模式后的终端行为（含 Windows），通过 `chat.steering` 开关回退。

Draft 0.1 已经过一轮独立评审（结论 BLOCKED，5 项发现），本稿 Draft 0.2 已逐项修订（见修订记录）。按低暴露度只做一轮合并轴评审（SR1409），修订后不再开新的评审轮次。下一步：用户答复 Q1–Q4 并批准本设计 → 起草实施计划。批准设计不授权实施。
