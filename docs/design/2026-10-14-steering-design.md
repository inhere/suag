<!-- template_id: design; template_version: 1.1.1 -->
# 运行中插入消息（steering）与只中止当前回合 设计

> 状态：Approved 0.5（2026-10-14 用户批准：“批准 steering 设计 Draft 0.5”）/ 实施计划见 `docs/plans/2026-10-14-steering-plan.md`

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-14 | Claude（监督会话） | 初稿 |
| 0.2 | 2026-10-14 | Claude（监督会话） | 按独立评审 r1（codex job 20261014-114209-25b62278，结论 BLOCKED）修订：取消时交还本回合全部插话；续跑点的精确条件；chat 输入层按模式分流与回合期间消费者；插话在段交接/压缩中的语义；收件箱容量、`--steer` 回退与 `--wait` 完成点、验收口径 |
| 0.3 | 2026-10-14 | Claude（监督会话） | 记录用户对 Q1–Q4 的答复（均按建议）：提示符 `↳ `、不做 follow-up 键（suga-8pug）、Esc 中止不确认、按 S1/S2/S3 分阶段；ACP steering 登记为 suga-lkf7；会话间互发登记为 suga-hko7 |
| 0.4 | 2026-10-14 | Claude（监督会话） | 按子 agent 复评 r2（结论 BLOCKED）修订：用户选 N1 方案 B——放弃“同一回合续跑”，模型给出最终回复即正常结束，未送达插话作为下一回合（D3）；S1 先修 suga-0lxy（回合中途压缩条数不一致）（N2）；中止时退回的插话一律回到输入行、不自动开回合，`--wait` 与 trace 语义重定（N3）；回合中 raw 模式保留输出换行处理、回合中所有输出经输入源（N4）；输入层的 Ask 分流、Esc 超时、半行交接（N5）；收尾时由 agentcore 告知不取插话（N6）；yield、提前返回、stream-json、UserPromptSubmit、开关归属等（N7） |
| 0.5 | 2026-10-14 | Claude（监督会话） | 按改动确认 r3（结论 PASS_WITH_ISSUES）补 M1：输入行常驻期间的输出方式（只把完整行交给 `Terminal.Write`、未结束尾部缓冲、steer 模式下 spinner 并入状态行）与对应冒烟；r3 的 8 项 MINOR（m1–m8）列为计划阶段事项 |
| 0.6 | 2026-10-15 | suga（会话 sess_2610141itpnahmur8001o） | 注记：取消/失败回合由“整段不回放”改为前缀回放（docs/design/2026-10-15-interrupted-turn-replay-design.md），“交还全部插话”的理由随之变为“回放跳过插话条目”；本设计的退回规则与代码不变 |
| 0.7 | 2026-10-15 | suga（会话 sess_2610141itpnahmur8001o） | 部分实施 S2b（中止先行，回应 suga-nrf2）：回合中继续读键盘（不经行编辑器，避免输入行与模型输出互相打断——M1 由后续阶段负责），`chat.steering`（默认开）下 Esc / 单次 Ctrl-C 中止本轮、3 秒内两次 Ctrl-C 退出；raw 期间恢复 `OPOST|ONLCR`；审批提问期间让出键盘。插话送达收件箱、退回处理、`--steer`、屏幕模型与 wincons 场景仍待做（suag-mbwu） |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

关联：issue suag-mbwu；plan-20261014-102002-6f9b4d19（D 项）、plan-20261014-124412-a00ce9ff（D 项）。相关：suga-0lxy（回合中途压缩条数不一致，S1 前置）、suga-nrf2（Windows 回合中 Ctrl+C 无反应，S2 前置）、suga-wy40（会话短名称）。评审记录：`local/reviews/2026-10-14-steering-design-review-r1.md`、`-r2.md`（未入库）。参考：pi coding-agent 的 steering / follow-up / abort（`tmp/refer-src/pi-mono/packages/coding-agent/docs/`）。

### 思考模式与范围冻结

- thinking_mode：RIGOROUS。
- core_objective：用户在模型干活时能（1）插一句话纠偏，模型在下一次请求前看到；（2）只停掉这一轮，chat 不退出。
- allowed_scope：`pkg/agentcore`（向上下文构建暴露收尾状态）、`internal/personal`（会话收件箱、持久化、事件、压缩修复）、`internal/chat`（输入层、回合中的输入行、按键）、`internal/personal/process` 与 `internal/cli/send.go`（`--steer`）、trace。
- non_goals：ACP 的 steering（suga-lkf7）；会话间互发（suga-hko7）；follow-up 键（suga-8pug）；排队消息的跨进程持久化；gofer 侧改动。
- expansion_policy：DEFER_OR_REQUEST。
- review budget：低暴露度（个人工具、未投产、可回滚；SR1409）。已做 r1（codex）与用户要求的 r2（子 agent）；0.4 只对改动部分做一次确认。
- 停止条件：确认无 CORE_BLOCKING 后请用户批准，进入计划。

## 背景与目标

用户 2026-10-13 提出“运行中插入信息怎么做”。现状：模型一轮可以连续调用几十次工具，期间用户发现方向不对，只能等它跑完，或者按 Ctrl-C 把整个 chat 杀掉再续接。

用户可见目标：

1. **插话（steer）**：回合进行中输入行保持可用。键入一句话回车，模型在“当前这次回复及其工具调用完成后、下一次模型请求前”看到它并据此调整，本轮不中断。若模型此时已经给出最终回复，插话作为紧接着的下一回合运行。
2. **只中止本轮**：回合进行中按 Esc 或一次 Ctrl-C，只取消当前回合（会话保留、chat 不退出），回到提示符；3 秒内连按两次 Ctrl-C 才退出 chat，与提示符处一致。
3. **send 也能插话**：`suga send --steer` 把消息插进正在进行的回合；默认行为（回合结束后作为新回合）不变。
4. 插话可观测：chat 显示、trace 记录；续接、`suga trace` 都能看出这句话是在回合中插入的。

## 名词

- **回合（turn）**：一次 `Runner.Run`，包含多次模型请求和工具调用，直到模型给出最终回复或出错/取消。
- **步（step）**：回合内的一次模型请求（及其工具调用），受 `max_steps` 约束。
- **插话（steer）**：回合进行中提交、在下一次模型请求前进入上下文的用户消息。
- **追加（follow-up）**：回合进行中提交、等本轮结束后作为下一回合的消息（现有 `suga send` 默认行为）。
- **会话收件箱（inbox）**：Runner 内按会话保存的待插入插话；**已取出（taken）**：本回合已进入某次模型请求上下文的插话。
- **退回（returned）**：回合结束时未送达或随回合作废的插话交还调用方。

## 范围与非目标

范围：

- agentcore：在调用 `ContextBuilder.BuildMessages` 前让它知道本步是否处于收尾（N6）。不改变回合结束条件。
- personal：会话收件箱、取出登记与退回、插话的持久化与事件；修复回合中途压缩条数不一致（suga-0lxy）；插话在段交接/压缩中的语义。
- chat：输入层按模式分流；回合中保持行编辑；Enter 插话；Esc / 单次 Ctrl-C 中止本轮；退回插话的处理。
- send：`--steer`；协议加字段，旧客户端保持原行为。
- 观测：trace 事件、chat 渲染。

非目标：

- ACP 的 steering（D8，suga-lkf7）。`session/cancel` 已能只中止当前 prompt，ACP 行为不变。
- `suga run` 的插话：run 不注册会话 socket，无插话入口；stream-json 不新增行类型（N7）。
- 会话间互发（suga-hko7）、follow-up 键（suga-8pug）。
- 进程退出后保留排队消息；多行插话的特殊编辑器。
- 子 agent 的插话（插话只进入父回合）。

## 已确认事实与规范

以下在 v0.20.0 代码中核对，v0.21.0 未改动这些路径的语义；行号可能有偏移。

**chat 输入**

- 只有在读一行时才进入 raw 模式；回合进行中 `SetBusy(true)` 置 `turnActive` 并强制回到 cooked 模式（`internal/chat/chat_input.go:212-220、292-294`）。注释写明原因：回合中要让 Ctrl-C 产生 SIGINT、stderr 保留换行转换（`chat_input.go:105-108`）。x/term 的 MakeRaw 在 Unix 上清除 OPOST（r2 核对 `term_unix.go:33`）。
- 输入源是“一次 `Request` 读一行”，`events` 缓冲 1，读到首字节先发 activity 并等 `AckActivity`；回合进行中主循环阻塞在 `runner.Run`，没有消费者（`chat_input.go:47-103、350-411`）。
- raw 模式下 Ctrl-C 被 lineedit 读成 0x03 并返回 `ErrInterrupt`（`lineedit/terminal.go:806、962`；chat 将其与 EOF 一并视为退出，`chat_input.go:372-394`）；lineedit 把单独的 ESC 当作未完成序列（`lineedit/terminal.go:247-261`）。
- 回合中的 Ctrl-C 是 SIGINT：`cmd/suga/main.go:36` 取消进程级 ctx，chat 回合返回后 `return ctx.Err()` 退出（`internal/chat/chat.go` 回合错误分支）。chat 没有回合级 ctx。
- 已有“只结束本轮”的路径：审批提问时 Ctrl-C/Ctrl-D 返回 `errAskCancelled`，经 `abortingToolPolicy` 让回合失败，chat 显示“已取消”并继续（`internal/chat/ask.go:14`、`internal/personal/denial.go:116-125`）。Ask 通过 `events` 读答案、`flushInput`/`inputEpoch` 接管读取（`ask.go:48-190`）。
- chat 在回合中有直接写 stderr 的位置（如 hook 提示、错误输出，`chat.go` 约 185、379 行）。

**send 投递**

- `suga send` 经会话 socket 投递；chat 侧 `DeliverWait` 在忙时返回 `queued=true`，队列上限 8（`chat_input.go:129-148`，`internal/personal/process/process.go:18`）。投递只在两回合之间被 `nextChatInput` 消费，作为新的注入回合运行；`--wait` 在消息开始成为回合时收到第二个响应（`process.go:241-257`）；退出时未消费的消息被丢弃，`--wait` 退出码 3。

**回合中注入消息的现成机制（后台任务通知）**

- agentcore 每次模型请求前调用 `ContextBuilder.BuildMessages`（`pkg/agentcore/runtime.go:150`）。personal 的实现先 `takeTaskNotices`，把通知作为 `role=user` 消息插到当前位置，以 `EntryUser` 落库到当前 turn，但不加入 `state.Messages`（`internal/personal/context.go:225-229`，`internal/personal/tasks.go:54-87`）。
- 回合中途压缩的 `items()` 要求落库条数与内存条数相等，有通知时报 `turn … has N stored messages but N-1 in memory`（`internal/personal/compact.go:141-165`；r2 以测试复现）→ suga-0lxy。
- 已完成回合（含 yielded，按 completed 存储）的条目在后续回合回放；被取消/失败回合的条目不回放（`context.go:73-90`）。（2026-10-15 更新：取消/失败回合改为前缀回放，见 docs/design/2026-10-15-interrupted-turn-replay-design.md；插话条目仍不回放）
- 收尾状态 `wrappingUp` 只在 agentcore 回合循环内（`runtime.go:124-140`），`BuildMessages` 看不到。

**取消的记录**：`Runner.finish` 以 `ctx.Err() != nil || errors.Is(runErr, context.Canceled)` 判定取消，turn 记 `cancelled`，会话写回 `open`（`internal/personal/runner.go` finish）。`Run` 在进入 agentcore 前有几处直接返回（r2 指出约 452/461/470 行）。

**ACP**：同一会话 prompt 进行中再发 `session/prompt` 返回 InvalidParams（`internal/acpserver/server.go:197-202`）。

**适用规范**：SR1207、SR1204、SR1402.1、SR1433、GR104（`chat_input.go` 约 450 行、`lineedit/terminal.go` 约 1000 行，新增逻辑另起文件）。

## 总体方案

主线：**插话进入 Runner 的会话收件箱 → 下一次 `BuildMessages` 时取出、落库、插入上下文 → 回合按原有条件结束；结束时未送达的插话退回调用方，正常结束时 chat 立即把它们作为下一回合运行**。chat 键盘、`suga send --steer` 只是往收件箱放消息的入口；中止本轮是给 chat 加回合级 ctx，并让回合中的 Ctrl-C/Esc 成为按键事件。

不改变 agentcore 的回合结束条件（方案 B）：回合状态始终单一（completed / cancelled / failed），用户已看到的最终回复不会因后续插话被作废。

## 架构

```text
 键盘 Enter ─┐                          ┌─ Runner.inbox[session]（容量 8）─┐
             ├─> Runner.Steer(sid,msg) ─┤  互斥 + closing 标志              │
 send --steer┘   accepted=false ──> 调用方按 follow-up 处理               │
                                                                           ▼
 agentcore 回合循环: buildPrompt → BuildMessages(非收尾步：取出插话、落库、发事件) → 模型
                                                                           │
 回合结束（finish 置 closing）→ RunResult.ReturnedSteers ──> chat：
     completed：作为下一回合输入立即运行（经现有投递优先路径）
     cancelled/failed：放回输入行，不自动运行
```

### agentcore：向上下文构建暴露收尾状态（N6）

- 回合循环在调用 `BuildMessages` 前把本步是否收尾写入 `RunState`（如 `RunState.WrapUp string`，空表示非收尾）。不新增续跑点，不改变结束条件。
- personal 的 `takeSteers` 在收尾步不取插话：收尾请求只要求作答，插话留在收件箱，回合结束时退回。

### personal：会话收件箱（取出登记、结束退回）

- 生命周期：收件箱在 `Run` 即将调用 agentcore 运行时打开，由 `Run` 内的 `defer` 关闭；`Run` 在此之前的提前返回不打开收件箱，此期间 `Steer` 返回 false（N7）。
- `Runner.Steer(sessionID, Steer{ID, Text, Source}) (accepted bool, err error)`：仅当本进程正在为该会话运行回合、收件箱已打开且未 `closing`、未满（容量 8，D11）时接受。满了返回 `ErrSteerQueueFull`：键盘提示“插话过多，请等本步结束”，send 返回现有的 `queue_full` 响应（`process.go:232`）。其余情况返回 false。
- `BuildMessages` 中 `takeSteers`（与 `takeTaskNotices` 并列，非收尾步）把收件箱全部插话移入 `taken`：作为 `role=user` 消息（metadata `suga.steer: true`、`suga.source: keyboard|send:<from>`、`suga.steer_id`）以 `EntryUser` 落库到当前 turn，并通过 turn 的 Emit 发 `steer_delivered` 事件、记 trace。
- 关闭时（`finish` 开始，锁内置 `closing`）计算 `RunResult.ReturnedSteers`：
  - 回合以 completed 存储（含 yielded、wrap-up 收尾）：退回收件箱中未取出的插话；`taken` 已进入回合历史，随回合回放。
  - 回合 cancelled / failed：退回 `taken + inbox` 全部插话（被取消/失败回合的条目曾整段不回放，r1 发现 1；2026-10-15 起改为前缀回放但跳过插话条目，退回规则不变）。
  - 每条退回的插话发 `steer_returned` 事件、记 trace。
- 测试：取消发生在 `BuildMessages` 与模型请求之间；收尾步不取插话；`closing` 之后到达的 `Steer`；收件箱已满；提前返回路径。

### personal：回合中途压缩与插话语义（N2，含 suga-0lxy）

- S1 先修 suga-0lxy：`items()` 与 `compose` 统一按“落库条目 = 内存消息 + 本回合注入条目（任务通知、插话）”对齐，注入条目按其插入位置参与切分；切点前的注入条目不再被丢弃。补“通知/插话之后触发压缩”的测试（压缩成功、注入条目位置正确）。
- 插话不是回合的原始请求：`recentRequests`（`segment_handoff.go:86-145`）只计回合原始 prompt（并顺带排除任务通知，修正其被当作用户请求的问题）；交接包新增“本回合插话”小节，按时间列出带 `suga.steer` 的消息及来源。压缩/交接摘要输入中插话前加 `[插话]` 标签。
- 测试：压缩发生在插话前、后两种顺序时，交接包中原始请求与插话各在其位。

### chat：输入层按模式分流（N4、N5）

S2 先重构输入层，再接入插话与中止。

1. **单一读取协程 + 模式**。保留一个读取协程，按模式（`prompt` / `steer` / `ask`）解释输入，同一时刻只有一个模式：
   - `prompt`（回合之间）：现状不变（activity 握手、Ctrl-C/Ctrl-D 规则）。
   - `steer`（回合进行中）：Enter → `steer` 事件；Ctrl-C → `abort` 事件（3 秒内第二次 → `exit` 事件）；单独的 Esc → `abort` 事件；Ctrl-D 忽略。不发 activity 事件。
   - `ask`（审批提问）：进入时把 `steer` 模式已键入的半行存入 `savedLine`，答案只经 Ask 自己的应答通道返回，回合消费者看不到；回答后恢复 `steer` 模式并回填半行。
   事件经按模式区分的通道分发：回合消费者只接收 `steer/abort/exit`，Ask 只接收自己的答案，避免审批答案被当成插话（N5）。
2. **Esc 判定**：读取协程读到 0x1b 后，若 50ms 内没有后续字节，向 lineedit 送出“单独 Esc”键；有后续字节则按转义序列处理。lineedit 相应修改：单独 Esc 不再挂起等待序列（`terminal.go:247-261`）。
3. **回合期间的消费者**：chat 在回合开始时启动回合输入协程（与 `runner.Run` 并行），`steer` → `Runner.Steer`（不接受时暂存为“待退回”）；`abort` → 取消回合级 ctx；`exit` → 取消回合并标记退出。回合结束时停止消费者并切回 `prompt`。
4. **输出方式（r3 M1）**：`Terminal.Write` 假定写入以换行结尾、写完从第 0 列重画提示符（`lineedit/terminal.go:694-739`），而流式回复按增量写出、spinner 每帧写 `\r\x1b[2K`，都不带换行。回合中输入行常驻后：
   - 流式输出只把完整行交给 `Terminal.Write`，未结束的尾部先缓冲，回复结束（`message_stop`）或换行时再输出；
   - steer 模式下不单独绘制 spinner，等待提示并入输入行上方的一行状态（或停用），避免与输入行争用同一行；
   - 超出行宽的行按终端宽度折行计算，避免光标行数算错留下残行。
   冒烟必须覆盖“长流式回复（含超宽行）+ 正在输入插话”，并做反向验证。
5. **终端模式（N4）**：回合中使用“输入 raw、输出保留换行处理”的模式——Unix 上在 MakeRaw 之后恢复 `OPOST|ONLCR`；Windows 上 MakeRaw 只改输入句柄，输出处理不受影响（实施时核实）。回合中 chat 的所有输出（含 hook 提示、错误）都经输入源写出（`Terminal.Write` 在输入行上方输出并重绘输入行），不再直接写 stderr。
6. **半行交接（N5）**：回合结束切回 `prompt` 时，`steer` 模式的半行经 `savedLine` 带入提示符读取；Ctrl-C 中止本轮不清除半行。
7. **回合级 ctx**：`runner.Run` 使用 `context.WithCancel(ctx)` 派生的回合 ctx；`abort` 只取消它，显示“已中断本轮”，回合记 cancelled。raw 模式下 Ctrl-C 不产生 SIGINT，工具子进程随回合 ctx 取消被进程树清理（与现有超时路径相同）。SIGTERM 仍终止整个 chat。
8. **显示**：`steer` 模式提示符 `↳ `；Enter 提交后在输出区显示淡色“[插话] <文本>（等待送达）”，收到 `steer_delivered` 后以输入行背景样式（suga-lebs）显示为已送达。
9. **开关**：`chat.steering`（默认开）在 S2 引入；关闭时 `SetBusy(true)` 保持现行为（cooked，Ctrl-C 为 SIGINT）。
10. **Windows**：S2 前置定位 suga-nrf2；Windows 控制台 raw 模式下 Ctrl+C 作为 0x03 读取在技术上可行（r2 核对 x/term），真机行为需确认，无法自动化部分记 UNVERIFIED_REAL_ENV。
11. 非终端模式（stdin 不是 tty）不变：不支持插话，Ctrl-C 仍为 SIGINT。

### chat：退回插话的处理（D3、D4、N3）

- **回合 completed**：退回的插话（键盘与 send 来的，按到达顺序）作为下一回合输入立即运行，走现有投递优先路径：若此时有 Stop hook 等待，按现有规则中断等待（其回复并入下一回合上下文）。多条插话合并为一个 prompt，每条带来源标注。
- **回合 cancelled / failed（含 Esc 中止）**：退回的插话全部放回输入行（键盘与 send 来的都放，send 来的带“[来自 x]”标注，多条用换行连接，置于半行之前），**不自动运行**——中止就是让 agent 停下。chat 显示“已中断本轮，N 条插话已放回输入行”。
- 已排队的普通 follow-up（非插话）保持现有行为：在下一次提示符时自动运行。（如需中止后也暂停 follow-up，另行提出。）
- 退回的插话不进入投递队列，因此不会与队列容量 8 叠加溢出。

### send：`--steer`

- `process.Request` 加字段 `Mode`（`"follow_up"` 默认 / `"steer"`）；旧客户端不带该字段即为 follow-up，行为不变。
- `--steer` 时 chat 先尝试 `Runner.Steer`：接受则第一响应为 `steered`；会话空闲、回合正在结束、或关闭了 `chat.steering`，则按 follow-up 排队并响应 `queued`（客户端打印“未插入当前回合，已排队”）；收件箱已满返回 `queue_full`。
- `--wait` 的完成点（N3 重定）：
  - 插话被取出送达模型（`steer_delivered`）→ 第二响应 OK；
  - 未取出就在 completed 回合结束时退回、并作为下一回合运行 → 在该回合开始时 OK；
  - 未取出就在 cancelled/failed 回合退回（放回输入行、不运行）→ 第二响应 `discarded`，退出码 3；
  - 已送达后回合才被取消：`--wait` 已经返回成功，之后的退回只记 trace（`steer_returned`），这是已知限制，写入文档。
- S3 在 S2 之后实施（依赖 `chat.steering` 与退回处理）。

### 观测

- trace：每条插话一条 `steer_delivered` 事件（attrs：steer_id、source、chars、step）；被退回时另记一条 `steer_returned`（attrs：steer_id、reason=completed_untaken|cancelled|failed、disposition=next_turn|input_line）。`suga trace` 把带 `suga.steer` 的 user 条目标为“插话”。
- stream-json：不新增行类型（run 无插话入口）。
- hook：插话不触发 `UserPromptSubmit`（不是新回合的 prompt）；退回后作为下一回合运行时，该回合照常触发。hook 可从 transcript 看到插话条目。
- chat 回合结束的状态行显示本轮插话次数（有时）。

## 关键流程

### 插话在工具调用之间送达

```text
用户 Enter "别改 config，只改 README"
  chat 回合消费者 → Runner.Steer(sid) → inbox=[s1], accepted
  ...当前工具调用完成...
  agentcore（非收尾步）→ BuildMessages → takeSteers：落库 EntryUser{suga.steer}，事件 steer_delivered
  模型请求含该消息 → 模型调整方向；chat 显示“[插话] 别改 config，只改 README”
```

### 模型正好给出最终回复

```text
模型返回无工具调用的回复 → 回合 completed（不续跑）
  finish 置 closing → ReturnedSteers = inbox 中未取出的 [s2]
  chat → s2 作为下一回合输入立即运行（Stop hook 等待按现有规则被中断）
插话在 closing 之后到达 → Steer 返回 false → chat 回合消费者把它作为下一回合输入
```

### 只中止本轮

```text
回合中按 Esc → abort → 取消回合 ctx → 进行中的模型请求/工具调用随 ctx 结束
  → finish：turn=cancelled，会话 open，ReturnedSteers = taken + inbox
  → chat 显示“已中断本轮，N 条插话已放回输入行”，插话（含 send 来的）放回输入行，回到提示符，不自动运行
```

## 安全、数据、运维与回滚

- 安全：插话与普通用户输入同权，不绕过审批与只读模式；`--steer` 只能投递到同用户可访问的会话 socket，权限边界与现有 send 相同（socket 目录 0700 / Windows ACL）。
- 数据：插话以 `EntryUser` 落库，新增 metadata 键，不改 schema；被取消回合的插话不随该回合回放（与现有一致；2026-10-15 起该回合按前缀回放，但回放跳过插话条目，与退回规则互补）。suga-0lxy 的修复改变中途压缩的切分，需回归现有压缩测试。
- 兼容：send 协议新字段可缺省；stream-json 不变。
- 运维：`chat.steering`（默认开）为回退开关；关闭时回合中恢复现行 cooked 行为，`send --steer` 退化为 follow-up。
- 回滚：各阶段独立提交；关闭 `chat.steering` 即回到现行 chat 行为。
- 平台风险：Windows 控制台 raw 输入下 Ctrl+C/Esc 的读取与输出处理需真机确认；suga-nrf2 须在 S2 前定位。无法自动化部分按 SR1402.1 记 UNVERIFIED_REAL_ENV。

## 决策

- **D1 注入路径**：复用后台任务通知的路径（`BuildMessages` 中取出、`EntryUser` 落库、原位回放），并在 S1 修复其中途压缩缺陷（suga-0lxy）。
- **D2 键位**：回合中 Enter = 插话；Esc 或单次 Ctrl-C = 中止本轮，不需确认；3 秒内两次 Ctrl-C = 退出 chat；Ctrl-D 在回合中无效。回合中的提示符 `↳ `。（Q1、Q3 用户已确认）
- **D3 模型已给出最终回复时（用户 2026-10-14 选 r2 N1 方案 B）**：回合正常结束，不在同一回合续跑；未送达的插话作为紧接着的下一回合运行。理由：回合状态单一，最终回复与本轮工作不会因后续插话被作废；无需 agentcore 续跑点。代价：两回合之间照常有回合结束与 Stop hook（等待会被插话中断）。
- **D4 插话不丢、中止即停**：completed 回合退回未取出的插话并作为下一回合运行；cancelled/failed 回合退回全部插话并放回输入行，不自动运行。
- **D5 收尾优先**：收尾步不取插话（agentcore 经 `RunState` 告知），剩余插话按 D4 退回。
- **D6 每步全部送达**：一次 `BuildMessages` 取出收件箱中全部插话，各自一条 user 消息；不提供 one-at-a-time 配置（SR1405）。
- **D7 follow-up 键**：不做（Q2，suga-8pug）。
- **D8 ACP 延后**：ACP InvalidParams 行为不变（suga-lkf7）。
- **D9 输入层先重构**：单一读取协程按模式分流事件，回合期间由独立消费者处理 `steer/abort/exit`，Ask 答案走独立通道；Esc 50ms 判定；回合中输入 raw、输出保留换行处理，所有输出经输入源。
- **D10 插话不计入原始请求**：段交接的“最近用户请求”只含回合原始 prompt，插话单列，压缩输入中带 `[插话]` 标签。
- **D11 收件箱容量 8**：满了明确拒绝；退回的插话不进投递队列。
- **D12 分阶段实施（Q4 用户已确认）**：
  - S1 内核：修 suga-0lxy；`RunState` 收尾状态；收件箱、取出登记与退回、`ReturnedSteers`；段交接语义；trace 事件。验收：单测（含取消竞态、收尾步、提前返回、插话后压缩）。
  - S2 chat：前置定位 suga-nrf2；输入层按模式分流、Esc 判定、终端模式、回合消费者、插话与中止、退回处理、`chat.steering`。验收：Linux pty 冒烟 + 反向验证为必选；Windows 控制台真机确认，做不到记 UNVERIFIED_REAL_ENV。
  - S3 send：`Mode` 字段、`--steer`、`--wait` 语义。依赖 S1、S2。验收：单测 + 冒烟。
- **D13 不在本设计内**：会话间互发（suga-hko7）；`suga run` 与 ACP 的插话入口。

## 待确认事项

Q1–Q4（2026-10-14，均按建议）与 r2 N1（方案 B）均已由用户答复；当前无待确认事项。

计划阶段需落实的事项（r3 MINOR，见 `local/reviews/2026-10-14-steering-design-review-r3.md`）：m1 completed 回合退回插话与 deliveries/taskWake 的先后、合并 prompt 的 injected 标记、chat 先退出时 send 插话的 settle；m2 `Response` 增加 steered 及第二段响应条件、DeliverWaitFunc 带 mode；m3 Esc 判定需要字节泵协程或平台 VTIME，并保证 Ask 的 flushInput 仍有效；m4 `RunState.WrapUp` 为瞬时字段（不进 JSON），每步重新赋值；m5 恢复 OPOST 按平台取 termios 请求码，slog 直写 stderr 的处理；m6 已更正为 `ErrInterrupt`；m7 两次 Ctrl-C 的计时在 prompt 与 steer 模式间共用；m8 收件箱打开前的短窗口内键入的插话成为下一回合（接受，写入文档）。

<details><summary>原问题（存档）</summary>

- **Q1** 回合中的输入提示符样式：建议 `↳ `。
- **Q2** 是否需要 chat 回合中的 follow-up 键：建议先不做。
- **Q3** Esc 中止是否需要确认：建议不需要。
- **Q4** 实施分阶段 S1/S2/S3 并各自验收。
- **r2 N1** 最终回复后因插话续跑若被中止/出错会作废整回合：方案 A 续跑失败仍按最后最终回复完成；方案 B 放弃同回合续跑，插话作为下一回合。

</details>

## 结论与人工计划 Gate

方案以“会话收件箱（取出登记、结束退回）”为核心，回合结束条件不变（方案 B），chat 键盘（先重构输入层）与 `send --steer` 作为入口，ACP 与 run 不提供插话入口；主要风险在回合中 raw 输入后的终端行为（含 Windows），通过 `chat.steering` 开关回退。

评审经过：Draft 0.1 → r1（codex）BLOCKED → 0.2；0.3 记录 Q1–Q4；r2（子 agent）BLOCKED → 0.4（N1 方案 B 由用户选定）→ r3 改动确认 PASS_WITH_ISSUES（无 CORE_BLOCKING）→ 0.5 补 M1。下一步：请用户批准本设计 → 起草实施计划（落实 m1–m8）。批准设计不授权实施。
