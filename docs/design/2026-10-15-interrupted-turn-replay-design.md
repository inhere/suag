<!-- template_id: design; template_version: 1.1.1 -->
# 中断回合的上下文回放（失败/取消回合前缀回放 + 中断小结）设计

> 状态：Draft 0.1（2026-10-15）。用户已定方案 3（前缀回放与中断小结互补）；实施随本设计直接落地，无独立计划文档。

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-15 | suga（会话 sess_2610141itpnahmur8001o） | 初稿：按 issue suga-y0sk 的实测证据定规则；用户确认方案 3（两者互补） |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

关联：issue suga-y0sk（本设计的来源与实测证据）、suga-o26e（同族：取消/失败回合里已取出的后台任务通知丢失）、suga-lrqy（web 端复用同一套历史重建）。相关设计：`docs/design/2026-10-11-segments-projection-design.md`（分段与上下文投影）、`docs/design/2026-10-14-steering-design.md` D4（插话交还）。

### 思考模式与范围冻结

- thinking_mode：RIGOROUS。
- core_objective：回合因网络错误或取消而中断后，下一回合的模型仍然知道它做过什么；工具的副作用与上下文记录不再分离。
- allowed_scope：`internal/personal`（历史重建 `context.go`、回合收尾 `runner_finish.go`、新增 `replay.go`）、单测、`docs/`（本设计、README 中英、CHANGELOG、steering 设计 D4 注记）。
- non_goals：新增 store 条目类型或迁移；改变压缩（compaction）与上下文更新（context_update）的采纳规则；改变插话（steer）交还语义；`pkg/agentcore` 改动；ACP/web 展示改动；Gofer 侧改动。
- expansion_policy：DEFER_OR_REQUEST。
- review budget：低暴露度内部语义修正（个人工具、未投产、可回滚）；一次实现 + 单测 + 反向验证。

## 背景与问题

用户在会话 sess_2610141itpnahmur8001o 里发现：一次回合因网络错误重试 6 次后中断（`turn_1itqtmn30`，failed，313.5s），他只回了一句“继续”，但模型完全不知道上一回合做过什么，把自己改的东西当成“用户在工作区里的未提交改动”，并重做了一遍核对与验证。

实测数据：

| 回合 | 状态 | 条目 | 字节 | 工具调用 | 悬空 |
|---|---|---|---|---|---|
| `turn_1itqtmn30` | failed（`retries=5`，`retry_reason=network error`） | 15 | 84.5 KB | 7（`shell_exec`×4、`file_write`×3，含 `python tmp\patch\apply.py` 改了 7 个文件） | 无（7/7 有结果） |
| `turn_1itpnm38s` | cancelled | 40 | 101 KB | 19 | 未统计 |

两段合计 185.8 KB 记录在后续回合里不可见，而它们在磁盘上的效果都留着。

根因是两处设计选择叠加：

1. `internal/personal/context.go:96-112,153-160`：`loadHistory` 只采纳 `store.TurnCompleted` 回合的条目（`if !done[e.TurnID] { continue }`）。
2. `internal/personal/runner_finish.go:97-121`：回合可以以 `failed` / `cancelled` 结束（`:114-117` 注释确认“its entries do not replay”）。

“不整体回放未完成回合”本身有正当理由（回放悬空的 tool_call 会被部分 provider 拒绝；半截压缩不该被采纳），但实现把**整段**都丢了，没有区分“可靠前缀”和“残缺尾部”。结果是副作用与记录分离：模型可能重复劳动、误判改动来源，更糟时会回滚已完成的工作或重复提交/重复建 issue。

## 名词

- **中断回合（interrupted turn）**：以 `failed` 或 `cancelled` 结束的回合（含被存活证明判死、由 recovery 置为 `cancelled` 的回合）。
- **完整工具对**：一条带 tool_call 的 assistant 条目，加上本回合内它每个 CallID 的 tool 结果条目。
- **前缀（prefix）**：从中断回合开头到**最后一个完整工具对**为止、可以安全回放的部分。
- **悬空尾部（dangling tail）**：前缀之后的条目（首先是那次没有凑齐结果的 assistant tool_call），整体丢弃。
- **中断小结（interruption note）**：收尾时为中断回合写的一条用户条目，记录状态、已执行的工具与原因。
- **回放（replay）**：`loadHistory` 把条目重新纳入下一回合的上下文。

## 事实（2026-10-15 核对）

| 事实 | 位置 |
|---|---|
| 回合状态常量 `running/completed/failed/cancelled` | `internal/personal/store/types.go:24-28` |
| `ListTurns(ctx, sessionID, statuses...)`：不给状态即列全部 | `internal/personal/store/turns.go:60-84` |
| 历史重建只采纳完成回合；压缩/上下文更新随其回合采纳 | `internal/personal/context.go:96-171` |
| 回合收尾设置状态、写回合事件、把会话置回 open | `internal/personal/runner_finish.go:65-162` |
| 状态被置为 cancelled 的另一条路径：死进程恢复 | `internal/personal/recovery.go:57-134`（在下一回合开始前完成） |
| 条目按消息类型写入（user/assistant/tool） | `internal/personal/store/agentstore.go:28-42,83-103` |
| 注入条目（任务通知、插话）用 EntryUser + 元数据，靠 `isInjectedItem` 与真实用户消息区分 | `internal/personal/context.go:30-48`、`tasks.go:22-23`、`steer.go:30-44,195-208` |
| 插话交还规则：completed 只退未取出的，cancelled/failed 全退（因“其条目不回放”） | `internal/personal/steer.go:122-140,268-280`、`runner_finish.go:114-118` |
| 工具参数的显示摘要（脱敏、限长）已有实现可用 | `internal/personal/transcript.go:78-136`（`summarizeToolArgs`） |
| 上下文更新的不采纳是刻意的：下一回合会重算，绝不丢也不重复 | `internal/personal/segment.go:326-368`（注释 `:337-338`） |
| 压缩记录被采纳时才影响历史（摘要 + `first_kept_seq`） | `internal/personal/context.go:72-94,140-151` |

## 设计

### D1 回放范围由回合状态决定

- `completed`（含 `RunYielded`/`INPUT_REQUIRED`，见 `runner_finish.go:95-101`）：整段回放，与现状一致。
- `failed` / `cancelled`：按 D2 回放前缀，外加 D3 的小结。
- `running`：不回放。进程死亡留下的 `running` 由 recovery 在下一回合开始前置为 `cancelled`（`recovery.go:99-107`），届时按上一行处理；这一顺序保证“死进程做过的事”也会回放。
- 子 agent 会话同规则（同一 `loadHistory`）。

### D2 前缀截断规则：截在最后一个完整工具对之后

对中断回合的条目按 seq 升序扫描，逐个决定是否保留：

1. 用户消息：保留——除**插话**（`isSteerItem`）之外。插话按 D5 不回放，也不算截断点（跳过继续扫描）。
2. assistant 消息：若它带有 tool_call，则**本回合内它的每个 CallID 都有 tool 结果**才保留；否则在此截断，后续条目全部丢弃。不带 tool_call 的 assistant 消息直接保留。
3. tool 结果：保留（其对应的 assistant 已被保留；若没被保留，扫描早已在此前的 assistant 处截断）。
4. 其它类型的条目（`context_update`、`compaction`、小结本身）：不参与前缀，也不构成截断点（跳过）。

截断点之后的条目（含其中已完成的工具对）进入“丢弃”计数，由小结说明。

为什么截断而不是挑着保留：被丢弃的尾部往往与前面的内容有引用关系（模型可能接着上次的结论继续写），局部补洞会让上下文自相矛盾；而截断点之后的信息由小结给出最小必要交代。

### D3 中断小结条目

`finish` 在回合以 `failed` / `cancelled` 落库后写一条小结（`EntryUser` + 元数据 `suga.turn_interrupted`），内容固定由四部分组成：

```text
[上一回合<已取消|运行失败>——这不是新请求。中断的回合没有正常收尾，但它已经执行过的步骤仍然有效：
工作区、文件和提交可能已被它改动，继续之前请先核对这些的实际状态。
- 已执行的工具调用（共 N 次）：file_write path=tmp/patch/logging.go.new；shell_exec command=python tmp\patch\apply.py；…
- 另有 M 条记录没有进入上下文（中断发生在未完成的工具调用处）
- 原因：<错误或停止原因>]
```

约束：

- 工具清单取自该回合 assistant 条目的 tool_call 参数，用 `summarizeToolArgs` 生成 `name key=value` 形式（沿用 transcript 的显示口径），最多列 8 项，超出只写“共 N 次”。
- 原因取模型/运行错误（`runErr`），脱敏（`redact.Secrets`）并截断到 300 字符；`cancelled` 回合没有可读错误时省略该行。
- 没有任何工具调用时写“- 未执行任何工具调用”。
- 整条小结最终限长 1500 字符。
- 写入失败只记 `slog.Warn`，不影响回合收尾（与 transcript、事件的既有做法一致）。

### D4 小结一定被回放

小结是中断回合里 seq 最大的一条（收尾时写），因此前缀回放天然把它放在该回合条目的末尾；即使尾部被截断，小结也不属于“尾部”，规则 D2.4 明确它不参与截断判定。这样最小保证是：无论截断多远，下一回合至少能看到“上一回合做了什么、为什么停”。
- 边界：D4 保证的是“进入下一回合的视图”。若下一回合在开头就重新压缩（大中断回合的常见情形，见“风险”），小结会像其它历史一样被折叠进摘要、不再逐字可见；需要它必然逐字出现时，可以把它并入段上下文消息（后续可选，不在本期）。

### D5 插话、任务通知、上下文更新

- **插话不回放**：插话在 cancelled/failed 回合会全部交还给调用方（steering D4），如果同时回放就会重复。因此回放跳过 `isSteerItem` 的条目，`returnSteers` 的语义与代码不变。
- **任务通知不回放、退回任务管理器**：通知是回合内注入的事实（后台任务结束），但任务管理器只交付一次（TakeNotices 取出即清空）。cancelled/failed 回合在收尾时把本回合取出的通知退回任务管理器的待取队列（`ReturnNotices`，保持原顺序、排在新到的通知之前），下一回合重新取出后告诉模型；回放因此跳过 `isTaskNoticeItem` 的条目，否则同一任务的结束会在下一回合出现两次（与插话同理）。completed 回合不变：通知条目照旧随回放出现。issue suga-o26e（2026-10-16 修复）。
- **上下文更新（context_update）不采纳**：保持现状。它是从各处来源重新比对得出的记录，下一回合会重新计算并追加（`segment.go:337-338`），因此不采纳既不会丢也不会重复；随前缀回放它反而会与重算结果重复。
- **压缩（compaction）不采纳**：保持现状。压缩是“切掉历史”的语义，半截回合的压缩不该生效；不采纳时被它归纳的内容仍在历史里，没有信息损失。

### D6 观测与不变式

- trace/`suga export` 里：小结是一条普通条目（元数据 `suga.turn_interrupted`），回合的 `failed`/`cancelled` 状态本来就有；不需要新事件。
- 元数据让小结不被当成“用户请求”：加入 `injectedItemKeys`，于是段交接包的 recentRequests 与 `compact` 的注入条目处理都把它排除在外（`segment_handoff.go:128`、`compact.go:229-248`）。
- 不变式：**回放集合 ⊆ 该回合的 user/assistant/tool 条目，且总是“从回合开头起的连续前缀”**；压缩/上下文更新/插话的采纳与回放规则不因本设计改变。
- 不变式：`loadHistory` 对 `completed` 回合的输出与改动前逐字一致（回归由既有测试守住）。

## 行为对照

| 场景 | 改动前 | 改动后 |
|---|---|---|
| 回合 failed/cancelled，工具调用都完成 | 整段不回放，模型失忆 | 整段回放 + 小结 |
| 回合 failed/cancelled，最后一次工具调用没有结果 | 整段不回放 | 回放到该调用之前 + 小结（说明丢了多少） |
| 回合 failed/cancelled，只有用户请求、没有工具 | 整段不回放 | 用户请求 + 小结 |
| 插话（steer）交付后被取消 | 交还输入行 | 交还输入行（不回放，行为不变） |
| 任务通知（无论在前缀内还是在被丢弃的尾部） | 丢失 | 退回任务管理器，下一回合重新取出（只出现一次） |
| 死进程留下的 running 回合 | 记为 cancelled 且不回放 | 记为 cancelled 后按前缀回放 |

## 验收与验证

- 单测（`internal/personal`）：
  1. 失败回合：7/7 工具对 → 全部回放，顺序与 seq 一致；
  2. 失败回合：最后一个 assistant 的工具调用缺结果 → 截断在该条之前，`dropped` 计数正确；
  3. 取消回合：同规则；
  4. 插话条目在失败回合里既不在回放结果里、也不造成截断；
  5. 任务通知在 cancelled/failed 回合既不在回放结果里、也不造成截断，且被退回任务管理器（下一回合只取到一次）；completed 回合不变；
  6. 失败回合的 `compaction` / `context_update` 仍不采纳（既有测试继续通过）；
  7. 小结：`finish` 后存在一条 `EntryUser` + `suga.turn_interrupted`，文案含状态、工具清单与原因；无工具调用时是“未执行任何工具调用”；
  8. completed 回合的历史重建与改动前一致。
- 反向验证：临时把前缀回放的判定改为“始终 false”，第 1–5 条必须失败；临时去掉小结写入，第 7 条必须失败。
- 冒烟（Linux/macOS，`make smoke-chat`）：新增场景——`fakellm.py` 先回一次 `file_write` 工具调用，随后连续失败到回合中断（需要给假模型加“从第 N 个请求起失败 M 次”的能力），断言下一回合的上下文里能看到那次工具调用与小结点（用 `suga trace --json` 与下一回合请求体）。本机为 Windows，此项待 Linux 环境执行。
- 真实模型：本设计不涉及 provider 协议，不需要真实模型；但发布前建议跑一轮 `chat_retry.py` 既有冒烟确认无回归。

## 风险与回滚

- **上下文变长**：中断回合的前缀现在进历史（本例 84.5 KB）。缓解：段断点/压缩本来就是按体积触发的，超阈值会照常压缩；若观察到大回合中断后立即触发压缩，可考虑只回放“工具调用摘要 + 正文”并外置工具结果（后续可选优化，不在本期）。
  - 更确定的一层：**若中断回合自身在中途压缩过，回放必然让下一回合在开头再压缩一次**——带回来的内容量不少于当初触发那次压缩的量。这是既有机制的正当反应，不是缺陷；`internal/personal/segment_test.go` 的 `TestSegmentRecordAdoptedWithTurn` 就落在这个情形上（它已按新行为更新：下一回合渲染的是自己新写的记录，而不是失败回合的那份）。
- **重复劳动的反面**：模型可能把上一回合未做完的部分当成已完成。缓解：小结明确“没有正常收尾”“请先核对实际状态”，并且被截断的尾部在计数上可见。
- **回滚**：三条改动各自独立（`replayablePrefix` 的调用、小结写入、`injectedItemKeys` 追加）；去掉它们即回到改动前行为，无数据迁移。
