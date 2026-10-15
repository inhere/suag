<!-- template_id: design; template_version: 1.1.1 -->
# 历史折叠 设计

> 状态：Draft 0.2 / 已批准进入计划（2026-10-12 用户：“批准设计 0.2，Q1–Q3 按建议”）

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-12 | Claude | 初稿 |
| 0.2 | 2026-10-12 | Claude | 按合并评审修订：被折叠且原有思考内容的消息打 `suag.reasoning_folded` 标记，OpenAI provider 据此仍按思考模式回传 `reasoning_content`（CB1）；`fold` 事件的基准改存会话 meta，新回合不再漏记、新段清零（CB2）；“步”定义为助手消息（并行调用各算一步）；配置下限校验 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

## 背景与目标

思考模式 RIGOROUS；核心目标：把已验证的“历史折叠”从实验开关变成正式、默认开启的功能，降低长任务每步重发的上下文。scope freeze：折叠规则、默认值与配置、可观测性、文档与评测说明（见“范围”）；review budget：一轮合并轴评审（低暴露度，SR1409）；停止条件：用户批准。

来源（[研究记录](../research/2026-10-11-suag-vs-omp-loop-analysis.md) 的 R3 与“历史折叠实验”两节）：

- 长任务的 prompt 消耗主要来自每一步重发整段历史；历史里思考内容占 58–72%、整文件写入参数占 16–19%，工具结果只占 10–19% 且单条都在转存阈值以下，所以输出转存不起作用。
- 实验开关（cb35c89）在 T9 上 A/B（command-code，同一二进制，各 3 轮）：每次请求的上下文 73k → 37k，prompt 总量 −59%，耗时 −35%；未命中输入 +48%，按缓存读为未命中价格 1/10 折算输入侧成本 −40%；通过率未变差（0/3 → 2/3，样本小，不归因）。
- 网关实验：较早步骤的 `reasoning_content` 清空后仍被接受，模型行为正常。

用户可见结果：长任务默认更省、更快（每次请求的上下文约减半），行为可配置、可关闭；存储的会话历史完整不变，`suag trace` 能看到每次折叠。

## 名词

- **请求视图**：每一步发给模型的消息列表，由 `contextBuilder.compose` 从段上下文、历史与本回合消息组装；不是存储的历史。
- **步**：请求视图里的一条助手消息。一次模型调用发出 N 个并行工具调用时存为 N 条助手消息，思考内容与正文只在第一条上，按 N 步计。
- **折叠**：在请求视图中去掉某一步的思考内容，并把它过长的字符串参数换成一句说明；存储不变。
- **保留窗口（keep）**：最近不折叠的步数。
- **批次（every）**：折叠边界只在步数跨过 `every` 的整数倍时前移，两次前移之间请求前缀不变，缓存可复用。
- **折叠边界**：`((步数 − keep) / every) × every`（整除，下限 0）；序号小于它的步被折叠。

## 范围与非目标

范围：

1. 折叠规则沿用实验实现：去掉边界之前各步的 `Reasoning`；工具参数中长度超过 1000 字符的字符串（含 `edits` 等嵌套结构）换成“[folded: N chars sent in an earlier step; re-read the file if you need its content]”。原有思考内容的步被折叠时，在消息元数据上打 `suag.reasoning_folded`（`llm.ReasoningFoldedMetaKey`）标记。
1a. OpenAI provider 判断“思考模式会话”时，除非空 `Reasoning` 外也认这个标记，保证折叠后仍为每条助手消息回传 `reasoning_content`。
2. 默认开启：`context.fold_every`（默认 10，`0` 关闭）、`context.fold_keep`（默认 5）。启动时校验：`fold_every < 0` 或 `fold_keep < 1` 报配置错误。
3. 可观测性：折叠边界前移时记录一条 `fold` 事件，`suag trace` 显示。
4. 文档（README 配置表、`docs/usage/gofer-personal-agent.md`）与评测说明（默认开启；`--suag-set context.fold_every=0` 做对照）。

非目标（deferred）：

- 折叠工具结果（占比小、单条小，且模型更可能需要回看；先观察）。
- 改动分段、压缩与空闲整理（折叠是段内的视图规则，与之正交）。
- Anthropic 类型的思考内容（Claude provider 当前不回传 thinking 块，折叠只作用于参数）。
- OpenAI Responses API 的服务端历史（suag-uesr；启用后本地折叠对那部分不再生效，届时再定）。

## 已确认事实与规范

- 实验实现：`internal/personal/fold.go`（`foldHistory`/`foldValue`），在 `contextBuilder.compose` 末尾作用于请求视图；`personal.Config.FoldEvery/FoldKeep` 由 wiring 读取配置（cb35c89，测试 `fold_test.go`）。
- `openai.go` 只要请求中有一条非空 `Reasoning` 就判定为思考模式，并为每条助手消息发送 `reasoning_content`（清空的步发送空字段），DeepSeek 要求这一点；X2 验证网关接受清空的旧字段。并行调用时思考内容只在第一条助手消息上（`runtime.go` executeToolCalls），所以保留窗口可能不含任何非空思考内容，折叠后判定会失效——由范围 1/1a 的标记解决（评审 CB1）。
- 每个回合新建一个 contextBuilder（`runner.go`）；回合的最终回答在最后一次组装之后才写入，所以边界可能在新回合第一次组装时才前移（评审 CB2）。
- 上下文估算 `estimate` 使用上一次调用的 `input_tokens` 加视图 token 变化（按字符类型加权：ASCII 约 4 字节/token、CJK 一个字一个 token），折叠后估算随之变小，分段阈值按真实请求大小判断。
- 观测事件写入 `store.Event`（见 `compact.go` 的 compaction 事件），`trace.go` 渲染。
- 规则：SR1403（最小充分）、SR1409（低暴露度评审封顶）、SR1107（设计 → 计划 → 实施）。

## 总体方案

把实验开关转正：规则不变，默认值改为实验验证过的 10/5，补上思考模式标记、观测事件与文档。实现涉及 `internal/personal`（规则、标记、事件与基准）、`pkg/agentcore/llm`（思考模式判断与标记常量）、`internal/wiring`（默认值与校验）与 `internal/personal/trace.go`（显示），业务逻辑约 4–5 个文件、150 行以内。

## 架构

```text
Runner.Run（每回合）
  └─ contextBuilder.BuildMessages（每步）
       ├─ compose：段上下文消息 + 历史 + 本回合消息
       │    └─ foldHistory(view, every, keep)   ← 只改请求视图
       ├─ estimate / 分段阈值（按折叠后的视图）
       └─ 边界前移时 rec.record(fold 事件)
存储（entries）：始终完整，不经过折叠
```

## 关键流程

1. 每一步组装请求视图后计算折叠边界；边界为 0 时视图不变。
2. 边界前移（每 `every` 步一次）时，被折叠的步去掉思考内容与长参数；这一次请求的前缀从第一个被折叠的步起与上一次不同，需要重新缓存，之后 `every` 步内前缀保持不变。
3. 边界大于基准时记录 `fold` 事件，属性：`boundary`（被折叠的步数）、`steps`（视图中的步数）、`chars_before`/`chars_after`（视图字符数）、`segment`。基准存会话 meta（`fold_boundary` 与其所属段序号）：builder 创建时读取，记录事件后更新；meta 中的段序号与当前段不同（已开新段）时基准按 0 处理，`startSegment` 后同样清零。失败或取消的回合可能让边界回退，回退不记录，之后超过基准的前移照常记录。
4. 分段（阈值、`:compact`、空闲整理）开新段后，旧消息离开视图，折叠只作用于新段内的步。

## 安全、数据、运维与回滚

- 数据：存储的历史、ref、记忆与污染标记都不变；折叠只影响发给模型的请求。
- 行为风险：模型看不到早期思考与写入内容。缓解：说明文案提示需要时重读文件；保留最近 5 步完整；A/B 中通过率未变差。评测持续对照（`context.fold_every=0`）。
- 成本：每次边界前移多一次前缀未命中（实验中未命中 +48%），按缓存价格折算总体仍下降；对缓存读价格接近未命中价格的 provider 收益更大。
- 回滚：`context.fold_every: 0` 即恢复原行为，无需迁移。

## 决策

| ID | 决策 | 理由 |
|---|---|---|
| D1 | 折叠只作用于请求视图，存储不变 | 可回滚、可检索；与分段“投影在断点重算”一致 |
| D2 | 按批次前移边界（every/keep），不做逐步滑动窗口 | 逐步滑动会让每一步的前缀都变化，缓存全部失效 |
| D3 | 折叠思考内容与长字符串参数，不折叠工具结果 | 前两者占历史 75% 以上且只对当时有用；工具结果占比小、更可能被回看 |
| D4 | 默认开启，10/5 | 实验验证的参数；`0` 可关闭 |
| D5 | 边界超过基准时记录 `fold` 事件，基准存会话 meta、按段清零 | 可在 trace 与评测中核对折叠是否发生、省了多少；每回合新建 builder，基准不能只放内存（评审 CB2） |
| D6 | 折叠清空思考内容时打元数据标记，provider 据此保持思考模式 | 并行调用使保留窗口可能没有非空思考内容（评审 CB1）；与 `SegmentContextMetaKey` 同一机制 |

## 待确认事项

| ID | 问题 | 建议 |
|---|---|---|
| Q1 | 是否默认开启，参数是否用 10/5 | 已决（按建议）：默认开启，10/5 |
| Q2 | 是否同时折叠旧的工具结果 | 已决（按建议）：暂不做，列为 deferred |
| Q3 | 配置键名 | 已决（按建议）：沿用 `context.fold_every` / `context.fold_keep`；1000 字符阈值不开放配置 |

## 结论与人工计划 Gate

本设计把已验证的历史折叠转为默认开启的正式功能，范围限于规则转正、默认值、观测事件与文档。合并评审（1 轮，低暴露度）的 2 条阻塞问题已在 0.2 处理。需要用户批准设计（含 Q1–Q3）后进入计划；改动规模小，计划可用简短的实施清单。批准设计不授权实施。
