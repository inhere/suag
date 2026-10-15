<!-- template_id: plan; template_version: 1.2.0 -->
# 运行中插入消息（steering）与只中止当前回合 实施计划

> 状态：Draft 0.2 / 已批准实施（2026-10-14 用户：“steer 等这几个小项完成后再做”；开始于第四批小项完成之后）

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-14 | Claude（监督会话） | 初稿，依据已批准设计 Approved 0.5 |
| 0.2 | 2026-10-14 | Claude（监督会话） | 按计划评审 r1（PASS_WITH_ISSUES，`local/reviews/2026-10-14-steering-plan-review-r1.md`）修订：T3 同步改造现有 Ctrl-C 冒烟（steering 开/关两组）（F1）；T2 字节泵改为“只在有请求时读取、Esc 用 poll/VTIME 不留挂起读取”的读取契约（F2）；fakellm 分块流式与记录消息内容、最小屏幕模型断言、lineedit 终端宽度（F3、F4）；OPOST 恢复移到 T3 并补可失败验证（F5）；文件与事实更正（F6）；可追溯性补齐（F7）；T0 先做无行为变化的文件拆分（F8）；wincons、Esc 时序、P1 状态（F9） |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

### 思考模式与范围冻结

- thinking_mode：RIGOROUS。
- core_objective：按已批准设计交付（1）回合中插话，下一次模型请求前送达；（2）Esc / 单次 Ctrl-C 只中止本轮；（3）`suga send --steer`。
- allowed_scope：设计“范围”所列：`pkg/agentcore`（`RunState` 收尾状态）、`internal/personal`（收件箱、退回、段交接语义、事件）、`internal/chat` 与 `internal/chat/lineedit`（输入层、回合中输入与中止、输出方式）、`internal/output/livetext.go`、`internal/personal/process` 与 `internal/cli/send.go`、`internal/config/chat.go`、trace 展示、文档与冒烟。
- non_goals：ACP / run 的插话入口（suga-lkf7）、follow-up 键（suga-8pug）、会话间互发（suga-hko7）、发版。
- expansion_policy：DEFER_OR_REQUEST；实施发现按“操作性发现 / 纠正 / 语义修订 / 归属冲突”四类处理（见“完成 Gate”）。
- review budget：低暴露度（SR1409）；计划只做一轮合并轴确认。
- 停止条件：用户批准本计划并给出当前执行请求后才实施；任一阶段验收失败且需改变设计语义时停止并返回设计 Gate。

## 目标与完成定义

目标：实现设计 Approved 0.5 的 D1–D12。

完成定义（全部满足）：

1. S1、S2、S3 各自的任务完成标准全部满足，合入 suga 本地 main。
2. 容器：`gofmt`、`go vet`、`go test -race ./...` 通过，`make smoke-chat` 全部场景通过（含新增 `chat_steer.py`），新增冒烟场景均做过反向验证。
3. 主机（Windows）：`go test -count=1 ./...` 与 `staticcheck ./...` 通过；Windows 控制台插话/中止的真机确认完成，或以 `UNVERIFIED_REAL_ENV` 记录（SR1402.1）。
4. 设计 m1–m8 在对应任务中落实，或在 progress 中说明处理。
5. CHANGELOG「未发布」、README 中英、`docs/usage/gofer-personal-agent.md`、`skills/suga-guide/references/config.md` 同步。

## 范围、排除项与授权

- 范围：上节 allowed_scope；Git root 为 `inhere-tools/suga`，仅此一个仓库。
- 排除：发版、push、tag、gofer 侧改动、ACP 行为、`suga run` 行为（除共享代码路径的回归外）。
- 授权：本计划获批后，用户的当前执行请求授权本地实施与按功能点本地提交（SR1211）；实施经 gofer 派 suga 在 `.worktrees/` 下完成，由监督会话审核、冒烟、反向验证与合并。push / 发版不在本计划授权内，另行请求。
- `host_or_non_offline_action=NOT_APPLICABLE`

## 输入与批准证据

- 设计：`docs/design/2026-10-14-steering-design.md` Approved 0.5（提交 6130a00 起）；批准证据：用户 2026-10-14 原话“批准 steering 设计 Draft 0.5”。
- 评审：`local/reviews/2026-10-14-steering-design-review-r1.md`、`-r2.md`、`-r3.md`（未入库；r3 的 m1–m8 映射见“可追溯性”）。
- 前置缺陷：suga-0lxy（回合中途压缩与注入条目对齐，第四批 d1 job 实施中）；suga-nrf2（Windows 回合中 Ctrl+C，用户环境为 Zed 终端 + ConPTY）。
- 相关已合入能力：suga-lebs 输入行背景、suga-wy40 短名称、`scripts/wincons`（Windows 控制台驱动）。

## Capability Discovery

### Capability decisions

| capability_id | required_capability | searched_candidates | direct_reuse | thin_adapter_or_owner_extension | decision | proven_gap | duplication_and_lifecycle_risk |
|---|---|---|---|---|---|---|---|
| CAP-01 | 回合中注入用户消息并落库回放 | `takeTaskNotices`、`contextBuilder.compose`、`EntryUser` 回放 | 是 | 在 `internal/personal` 原位扩展 `takeSteers` | DIRECT_REUSE | none | 与任务通知共用对齐规则（suga-0lxy 修复后） |
| CAP-02 | 回合中读取按键并分流事件 | `chatInputSource`、`lineedit.Terminal`、`ask.go` 的 savedLine/flushInput | 部分 | 既有 owner 扩展：输入源增加模式与事件分流，lineedit 增加单独 Esc | OWNER_EXTENSION | 现有“一次请求读一行”无法在回合中产生事件（设计 D9） | 输入源单一 owner，不新建包 |
| CAP-03 | send 插话协议 | `process.Request/Response`、`DeliverWaitFunc` | 是 | 原位加字段 | OWNER_EXTENSION | none | 旧客户端缺省字段兼容 |

### New module candidates

| candidate_id | capability_id | proposed_module | searched_candidates | direct_reuse_gap | thin_adapter_or_owner_extension_gap | proven_gap | unique_owner_and_lifecycle | deletion_or_merge_handling |
|---|---|---|---|---|---|---|---|---|
| NONE | none | none | none | none | none | none | none | none |

### Rejected new tools

| rejected_candidate | capability_id | deletion_test_and_reason |
|---|---|---|
| NONE | none | none |

## 前置检查与 fail-closed 条件

- P1：suga-0lxy 已合入 main（第四批 d1，job 20261014-142926-0b09fdec，计划撰写时进行中），中途压缩在有任务通知时通过。未合入则 S1 不开始。
- P2：主仓库 `git status --short` 中没有与本计划 owner 路径重叠的他人改动；有则按“归属冲突”停止。
- P3：每个阶段开始前记录 workspace baseline：Git root `inhere-tools/suga`、branch `main`、HEAD、`git status --short`、待建 worktree 名与分支名。
- P4：容器内操作 worktree 只对单条 git 命令前缀 `GIT_DIR/GIT_WORK_TREE`，不 export（记忆 container-worktree-git-env）。
- P5：任一阶段的冒烟或反向验证无法成立（反向时检查不失败）→ 该阶段不合入，修冒烟或补测试后重验。
- 环境假设：主机 Windows 无 cgo/race/pty；容器 Linux 跑 race 与 pty 冒烟；`scripts/wincons` 只能驱动经典控制台，ConPTY（Zed 终端）需用户手工确认。

## 波次与依赖

| 波次 | 阶段 | 依赖 | 执行方式 |
|---|---|---|---|
| W0 | T0 无行为变化的文件拆分 | 无 | 1 个 suga job |
| W1 | S1 内核 | P1、T0 | 1 个 suga job（worktree `.worktrees/s1`） |
| W2 | S2a 输入层重构（无插话行为变化） | S1 合入 | 1 个 suga job |
| W3 | S2b 插话、中止、输出方式、退回处理 | S2a 合入 | 1 个 suga job |
| W4 | S3 send --steer | S2b 合入 | 1 个 suga job |
| W5 | 收尾：全量回归、Windows 真机确认、文档 | W4 | 监督会话 + 用户 |

S2 拆成 S2a/S2b：S2a 只重构输入层并保持现有行为（回归冒烟全过即可证明），降低一次性改动风险。T0 先把接近 1000 行上限的文件按职责拆分（F8），后续新逻辑放进新文件。

## 任务

### T0 无行为变化的文件拆分（F8）

- 文件：`internal/chat/lineedit/terminal.go`（989 行，拆出按键解析/重绘到同包新文件）、`internal/chat/chat.go`（`Run` 约 370 行，拆出回合执行到 `chat_turn.go`）、`pkg/agentcore/runtime.go`（972 行，拆出收尾相关函数到 `runtime_wrapup.go`）、`internal/personal/runner.go`（902 行，视需要拆出 finish 相关到 `runner_finish.go`）。
- 动作：纯移动与提取函数，不改逻辑；每个文件拆分后 ≤ 800 行。
- 验证（运行目录 `inhere-tools/suga`）：`go build ./...`、`go vet ./...`、`go test -race -count=1 ./...`、`make smoke-chat` 全过；`lineedit` 与 x/term 的逐字节 parity 测试通过；`git diff --stat` 只有移动（新增行≈删除行）。
- 完成标准：上述通过，四个文件均 ≤ 800 行。
- 依赖：无（可与第四批并行之后的 main 对齐）。

### T1 S1：收尾状态与会话收件箱

- 文件：`pkg/agentcore/api/types.go`（`RunState` 增加瞬时字段，命名区别于 `RunResult.WrapUp`（结束原因），如 `WrappingUp string`，`json:"-"`，m4）、`pkg/agentcore/runtime.go`（每步调 `BuildMessages` 前赋值）、新文件 `internal/personal/steer.go`（收件箱、`Runner.Steer`、`takeSteers`、退回计算）、`internal/personal/runner.go`（`RunResult.ReturnedSteers`；收件箱在调用 agentcore 前打开，`Run` 的提前返回都发生在打开之前（`runner.go:452-505`）；退回计算放在 `finish` 内，因为 cancelled/failed 状态在 finish 中才确定；`defer` 只兜 panic）、`internal/personal/context.go`（`BuildMessages` 中非收尾步调用 `takeSteers`；`contextBuilder` 目前没有 Emit（`context.go:167`），需把回合的 `req.Emit` 接入以发 `steer_delivered`）、`internal/personal/segment_handoff.go`（“本回合插话”小节、`[插话]` 标签）、`internal/personal/store/types.go`（`steer` 事件种类）、trace 展示（`internal/personal/traceweb.go:80/144` 与 `traceweb.html` 中按 `suga.steer` 标“插话”，与任务通知的 Notice 标注并列）。
- 动作：按设计“agentcore：向上下文构建暴露收尾状态”“personal：会话收件箱”“回合中途压缩与插话语义”“观测”实现；容量 8 与 `ErrSteerQueueFull`；completed（含 yielded）只退回未取出，cancelled/failed 退回全部；`steer_delivered` / `steer_returned` 事件；插话不触发 `UserPromptSubmit`。
- 验证（运行目录 `inhere-tools/suga`）：`go test -race -count=1 ./pkg/agentcore/... ./internal/personal/...`；新增测试至少覆盖：同一步多条插话全部送达（D6）、取消发生在 `BuildMessages` 与模型请求之间（可注入阻塞 provider）、收尾步不取插话、`closing` 后 `Steer` 返回 false、容量满、提前返回路径不打开收件箱、completed 退回未取出、cancelled 退回全部、插话后触发中途压缩、交接包中原始请求与插话各在其位、`RunState` JSON 不含 `WrapUp`。反向验证：去掉 cancelled 时退回 `taken` 的分支，对应测试失败。
- 完成标准：上述测试通过且反向验证成立；无 UI 变化，`make smoke-chat` 全过（行为不回归）。
- 依赖：P1。

### T2 S2a：chat 输入层按模式分流（行为不变）

- 文件：`internal/chat/chat_input.go`（模式 prompt/steer/ask、按模式分发的事件通道、Ask 独立应答通道）、新文件 `internal/chat/input_pump.go`（读取契约见动作，m3）、`internal/chat/ask.go`（改用独立应答通道；`flushInput` 在字节泵下仍生效：flush 同时丢弃泵内未交付字节，m3）、`internal/chat/lineedit/` 新文件（单独 Esc 键处理，不挂起等待序列；`terminal.go` 只加最少的分支，保持 <1000 行）。
- 动作：引入模式与按模式分发的事件通道，但 `SetBusy(true)` 在本阶段仍保持现有 cooked 行为（steer 模式先不启用），使外部行为与现状一致。读取契约（F2）：
  - 只在有读取请求时读取，两次请求之间不留挂起的 `Read`（与现状 `chat_input.go:350-363` 一致），回合中键入的内容不被提前读走；
  - 单独 Esc 的 50ms 判定不用“goroutine + 阻塞 Read + 超时”，而用不留挂起读取的等待：Unix 用 `poll`（或 termios VTIME/VMIN）判断 50ms 内是否有后续字节，Windows 用 `WaitForSingleObject` 等待控制台输入句柄；
  - activity / `inputEpoch` / `handing` 在把字节交给 lineedit 时计算，保持现有 Ask 的 tcflush + epoch 语义；
  - EOF 粘滞：读到 EOF 后后续请求立即返回 EOF。
  Windows 上“等待期间按 Ctrl+C”的返回值未核实，实施时以 `scripts/wincons` 实测并记录。两次 Ctrl-C 的计时改为输入源统一持有，供 prompt 与 steer 模式共用（m7）。
- 验证：`go test -race -count=1 ./internal/chat/...`（新增：Esc 判定（单独 Esc / 转义序列 / 50ms 边界，用可注入的等待函数）、两次请求之间无挂起读取（注入的 reader 记录调用）、Ask 期间输入不进事件通道、EOF 粘滞）；`make smoke-chat` 全过（证明行为不变）；`lineedit` 与 x/term 的逐字节 parity 测试仍通过。
- 完成标准：现有全部冒烟与单测通过；新单测通过；无用户可见变化。
- 依赖：T1 合入。

### T3 S2b：插话、只中止本轮、输出方式、退回处理

- 文件：`internal/chat/chat.go`（回合级 ctx、回合输入消费者、退回插话处理、回合中所有输出经输入源）、`internal/chat/chat_input.go`（`SetBusy(true)` 在 `chat.steering` 开启时切 steer 模式并保持 raw 输入 + 输出处理）、`internal/chat/render.go` 与 `internal/chat/spinner.go`（steer 模式下 spinner 并入状态行；插话的“等待送达/已送达”显示）、`internal/output/livetext.go`（输入行可见期间只交完整行，尾部缓冲，r3 M1）、`internal/config/chat.go`、`config/config.yaml`（`chat.steering`，默认 true）、`internal/chat/lineedit/`（steer 模式下按真实终端宽度排版：chat 在回合开始与窗口变化时调用 `SetSize`，目前固定 80 列（`terminal.go:130/141`），F3）、平台 termios 文件（回合中 raw 输入后恢复 `OPOST|ONLCR`，请求码按平台取，m5/F5）、`scripts/ptysmoke/fakellm.py`（新增选项：分块流式（每块延时）、超宽行、按需模式；在请求日志中记录每次请求的最后几条消息的 role 与内容摘要，F3/F4）、`scripts/ptysmoke/ptydrive.py`（最小 VT 屏幕模型：处理 CR/LF、`ESC[K`、`ESC[2K`、`ESC[nA/B`、`ESC[nG` 等，供“屏幕是否错乱”断言，F3）、`scripts/ptysmoke/chat_steer.py`（新）、现有 `scripts/ptysmoke/chat_tool_ctrlc.py` 与 `chat_agent.py`（F1）、`scripts/wincons/`（Windows 控制台插话/中止场景，F9）、`Makefile`。
- 动作：按设计“chat：输入层按模式分流”第 3–11 点与“chat：退回插话的处理”实现：Enter 插话、Esc/单次 Ctrl-C 中止本轮、3 秒内两次 Ctrl-C 退出；completed 回合退回的插话作为下一回合运行，并明确与 deliveries / taskWake 的先后：退回插话优先，合并 prompt 的 injected 标记按“含键盘插话则 false”（m1；理由：`suga.injected` 表示“非本人键入”，合并后的 prompt 只要含本人键入的插话，就应按本人请求对待，段交接的“最近用户请求”也应计入）；cancelled/failed 退回放回输入行不自动运行；回合结束状态行显示本轮插话次数（有时）；提示符 `↳ `；半行交接；slog 在回合中输出经输入源或暂缓（m5）；收件箱打开前的短窗口内键入的插话作为下一回合（m8，写入文档）。
- 验证：`go test -race -count=1 ./internal/chat/... ./internal/output/...`；termios 单测（恢复后 Oflag 含 OPOST|ONLCR）。
  现有冒烟改造（F1）：`chat_tool_ctrlc.py` 与 `chat_agent.py` 的 Ctrl-C 场景分为两组——steering 开：单次 Ctrl-C 显示“已中断本轮”且 chat 仍在、工具被中止（steer 模式下 raw 输入不向前台进程组发 SIGINT，所以“去掉 shell_exec 的 cmd.Cancel”此时是有效反向点）、再次 Ctrl-C（3 秒内）退出；steering 关（`chat.steering: false`）：保持现有断言。`scripts/wincons` 同步。
  `scripts/ptysmoke/chat_steer.py` 覆盖（判据须可证伪，F4）：工具调用期间插话，fakellm 日志显示**同一回合**紧跟 tool 结果之后的下一次请求中出现插话文本，且 trace 有 `steer_delivered`、本回合只有一个 turn 事件、模型给出最终回复后到达的插话作为下一回合、Esc 中止本轮后 chat 仍在且插话回到输入行、单次 Ctrl-C 中止本轮、两次 Ctrl-C 退出、审批提问期间键入不被当作插话、长流式回复（fakellm 分块+延时+超宽行）+ 正在输入插话时，用屏幕模型断言回复全文与输入行各在其位、无残行（M1）、回合中 hook 提示与 slog 输出同时正在输入插话时不覆盖输入行（F5）、提示符为 `↳ `、状态行显示插话次数、Esc 冒烟发送 `\x1b` 后至少等 100ms 再发其他字节（F9）、`chat.steering: false` 回到现行为；`suga trace --json` 中 `steer_delivered` / `steer_returned` 事件。反向验证（判据见上，确保会失败）：去掉“插话送入收件箱”→“同一回合送达”检查失败（插话会变成下一回合）；去掉“abort 取消回合 ctx”→“已中断本轮且 chat 仍在”失败；去掉“只交完整行”→屏幕模型检查失败；去掉 OPOST 恢复→hook/slog 场景的屏幕模型检查失败；steer 模式下去掉 cmd.Cancel →“工具被中止”失败。全量 `make smoke-chat` 通过。
- real-env：Windows 经典控制台用 `scripts/wincons` 扩展插话/Esc/Ctrl-C 场景；Zed 终端（ConPTY）由用户手工确认（步骤写入 `scripts/wincons/README.md`），未确认前记 `UNVERIFIED_REAL_ENV: Windows ConPTY 插话与中止（复现：go run ./scripts/wincons --console pty）`。suga-nrf2 在此阶段复核。
- 完成标准：单测、冒烟与反向验证全部成立；suga-nrf2 有结论（修复、或 UNVERIFIED_REAL_ENV 记录）。
- 依赖：T2 合入。

### T4 S3：`suga send --steer`

- 文件：`internal/personal/process/process.go`（`Request.Mode`、`Response.Steered`、第二段响应条件扩展到 steered，`DeliverWaitFunc` 带 mode，m2）、`internal/chat/chat_input.go`（按 mode 投递）、`internal/chat/chat.go`（send 插话送收件箱、不接受时转 follow-up；chat 先退出时未送达的 send 插话以 `discarded` settle，m1）、`internal/cli/send.go`（`--steer` 参数与输出）、`scripts/ptysmoke/chat_steer.py`（扩展）。
- 动作：按设计“send：`--steer`”实现四种 `--wait` 完成点与 `queued`/`steered`/`queue_full` 响应；旧客户端不带 Mode 时行为不变。
- 验证：`go test -race -count=1 ./internal/personal/process/... ./internal/cli/... ./internal/chat/...`（协议兼容：无 Mode 字段的请求、各 settle 路径）；冒烟：回合中 `suga send --steer --wait`：首个响应为 `steered`、在本回合结束之前返回 0（以 fakellm 延时保证回合未结束）、插话出现在同一回合的下一次请求中；空闲时退化为排队（首个响应 `queued`）；中止后返回 3；收件箱满时返回 `queue_full`（D11）。反向验证：去掉 steer 分流 → 首个响应不是 `steered`、`--wait` 在回合结束后才返回，检查失败（F4）。
- 完成标准：测试与冒烟通过；`docs/usage/gofer-personal-agent.md` 写明 `--steer` 与 `--wait` 语义，便于 gofer 透传。
- 依赖：T3 合入。

### T5 收尾：回归、真机确认与文档

- 文件：`CHANGELOG.md`、`README.md`、`README.zh-CN.md`、`docs/usage/gofer-personal-agent.md`、`skills/suga-guide/references/config.md`、设计稿状态行。
- 动作：容器全量 race + `make smoke-chat`；主机 `go test ./...` + `staticcheck ./...`；汇总 UNVERIFIED_REAL_ENV；更新文档；关闭 suag-mbwu（及 suga-nrf2 若已解决）。
- 验证：上述命令全部通过；文档包含键位、`chat.steering`、`--steer`/`--wait`、已知限制（送达后被取消的插话 `--wait` 已返回成功）。
- 完成标准：完成定义 1–5 全部满足。
- 依赖：T4。

## 回滚与恢复

- 每个阶段在独立 worktree 与分支开发，审核后 cherry-pick 合入 main；未通过验收的阶段不合入，worktree 保留直至处理。
- 恢复点：每阶段合入后的 main HEAD 记录在 progress（gofer plan todo note）。
- 运行时回退：`chat.steering: false` 回到现行 chat 行为；send 不带 `--steer` 行为不变。
- 不使用 `git stash` / `git reset` / `git checkout -- <他人路径>`；共享仓库只暂存本阶段 owner 文件。

## 人工 Gate

1. 本计划批准（当前请求）。
2. 当前执行请求：批准后由用户给出开始实施的请求（可与批准同句）。
3. T3 的 Windows ConPTY 手工确认需要用户在 Zed 终端操作。
4. push / 发版不在本计划内，另行请求（外部动作 Gate）。

## 可追溯性

| 设计条目 / 评审项 | 任务 | 验证 |
|---|---|---|
| D1 注入路径、suga-0lxy | P1、T1 | 插话后触发中途压缩测试 |
| D2 键位、Q1/Q3 | T2、T3 | chat_steer 冒烟（Esc、Ctrl-C、两次 Ctrl-C、`↳ ` 提示符）；改造后的 chat_tool_ctrlc / chat_agent |
| D3 方案 B（N1） | T1、T3 | completed 退回未取出测试；最终回复后插话成为下一回合冒烟 |
| D4 不丢且中止即停（N3） | T1、T3、T4 | cancelled 退回全部测试；中止后回输入行冒烟；`--wait` 返回 3 |
| D5 收尾优先（N6）、m4 | T1 | 收尾步不取插话测试；RunState JSON 测试 |
| D6 每步全部送达 | T1 | 同一步多条插话全部送达测试 |
| D7 不做 follow-up 键、D8 ACP 延后、D13 不在范围 | — | 非目标；无任务（suga-8pug、suga-lkf7、suga-hko7 保持开放） |
| 观测：状态行插话次数 | T3 | chat_steer 冒烟 |
| D9 输入层（N4、N5）、m3、m7 | T2 | 读取契约、Esc、Ask、EOF 单测；行为不变冒烟 |
| N4 / m5 输出处理 | T3 | termios 单测；hook/slog 冒烟与反向 |
| r3 M1 输出方式 | T3 | 长流式 + 输入中冒烟与反向验证 |
| D10 插话不计入原始请求 | T1 | 交接包测试 |
| D11 容量 8 | T1、T4 | T1 容量满测试；T4 `queue_full` 冒烟 |
| r3 M1 / 计划评审 F3 终端宽度 | T3 | SetSize 后超宽行屏幕模型检查 |
| 计划评审 F1 现有 Ctrl-C 冒烟 | T3 | steering 开/关两组断言 |
| D12 分阶段 | 波次 W1–W5 | 每阶段验收 |
| m1 退回与投递先后、settle | T3、T4 | 冒烟与 settle 单测 |
| m2 协议字段 | T4 | 协议兼容单测 |
| m6 ErrInterrupt 事实 | 已在设计 0.5 更正 | — |
| m8 短窗口 | T3 | 文档说明 |
| suga-nrf2 | T3 | wincons 场景；ConPTY 手工确认或 UNVERIFIED_REAL_ENV |

## 完成 Gate 与剩余工作

- 完成 Gate：完成定义 1–5 满足，且 UNVERIFIED_REAL_ENV 项已在交付说明中列出。
- 实施发现处理：新增路径/符号且不改变用户结果与风险 → 操作性发现，更新 progress 后继续；已批准 owner 内的一致性修正 → 纠正，记录并局部验证；改变产品行为、协议、状态机、验收或范围 → 语义修订，停止并返回设计/计划 Gate；命中他人改动或归属不清 → 归属冲突，停止。
- 剩余工作：suga-lkf7（ACP）、suga-8pug（follow-up 键）、suga-hko7（会话间互发）保持开放；发版另行请求。
