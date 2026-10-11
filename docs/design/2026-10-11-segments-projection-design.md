<!-- template_id: design; template_version: 1.1.1 -->
# 会话分段与上下文投影（第二期） 设计

> 状态：Draft 0.1 / 待人工计划批准

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-11 | Claude | 初稿 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

## 背景与目标

思考模式 RIGOROUS；核心目标：落实第一期总纲的“投影只在断点重算，断点之间只追加”——让缓存前缀跨会话稳定、把有损压缩改为面向当前任务的“分段交接”、在 chat 空闲时主动整理，并补齐量化这些改动所需的观测。scope freeze：W0–W4（见“范围”）；review budget：一轮合并轴评审（低暴露度，SR1409）；停止条件：评审无 CORE_BLOCKING 且用户批准。

来源：

- 第一期总纲与 D1（[状态、记忆与可扩展核心设计](2026-10-10-state-memory-extensibility-design.md)）。
- suag-sg9i：同模型对比中 suag 发送的 prompt 总量只有 omp 的 1/3，但小任务未命中缓存的输入是 omp 的 3–4 倍；63b3787 调整系统提示词内部顺序只降约 5%。
- suag-kxlq：用户提出“空闲一段时间后自动整理上下文”，并决定“chat 进程内计时，不依赖外部”。
- gofer 会话（2026-10-11）：suag job 用量已由 gofer 读取，请 suag 在 `turn_end` 也带累计用量、确认 ACP `usage_update` 结构，并约定工具调用次数字段 `tool_calls`。

用户可见结果：

1. 新会话的第一次请求能复用其他会话缓存过的前缀；评测中“首次请求未命中缓存的输入”明显下降（目标：小任务降到当前的一半以下）。
2. 上下文接近上限时，suag 开启新的一段：新段带着围绕当前任务整理的交接包（最后的用户请求原文、计划、涉及文件、可回读的 ref），旧段完整保留、可检索；行为可在 trace 中看到。
3. chat 空闲超过设定时间且上下文较大时，suag 在后台整理出新的一段；用户回来时直接继续，不再卡在回合中途的压缩上。
4. gofer 能在运行中途看到累计用量（`--max-tokens` 中途生效），suag-acp 的用量也能显示；评测能对比首次请求的缓存命中与多轮会话。

## 名词

- **稳定前缀**：每个请求开头、与工作区和会话无关的部分：系统提示词（身份、工作约定、skills 列表）与直接工具的定义。
- **段（segment）**：一个会话里两个断点之间的投影；以一条“段起始”记录开头。
- **段上下文消息**：每段开头的一条 user 角色消息，承载本段冻结的环境、项目规则、记忆索引、附加提示、延迟工具列表；段内不变。
- **交接包**：新段开头带入的、围绕当前任务整理的内容：摘要、最后一条用户请求原文、当前计划、涉及文件、相关 ref。
- **断点**：允许重建投影的时刻：会话开始、达到阈值、手动 `:compact`、空闲整理、跨会话 `--from`。
- **空闲整理**：chat 等待输入且空闲超过阈值时，在后台执行一次分段。

## 范围与非目标

范围（仓库 `inhere-tools/suag`）：

- **W0 观测与评测**：`turn_end` 事件带累计 `usage`；`result`/`turn_end` 的 usage 增加 `tool_calls`；核对并修正 ACP `usage_update` 与 gofer 的读取方式；评测支持多轮任务（同一会话多次 `run`）与“首次请求未命中缓存的输入”指标。
- **W1 稳定前缀**：系统提示词只保留稳定内容；环境、规则、记忆索引、附加提示、延迟工具列表移入段上下文消息；段内的规则/附加提示变化改为追加一条上下文更新消息，不再改写系统提示词；Claude provider 增加 `cache_control` 断点（先验证现状）。
- **W2 分段交接**：阈值与手动 `:compact` 改为开启新段：段起始记录（扩展现有 compaction 记录）、面向当前任务的交接包、新段刷新段上下文；trace 显示分段。
- **W3 空闲整理**：chat 进程内计时，空闲超过阈值且上下文估计超过比例时在后台分段；用户输入时取消；状态行提示。
- **W4 文档**：README、配置样例、CHANGELOG。

非目标：只读子 agent；权限与信任档位；编辑工具稳健性与文件检查点；空闲时提炼记忆候选（依赖审批流程，另起）；people（suag-frgt）；tool_search 加载 MCP 工具导致的工具数组变化（低频，接受其缓存失效）；跨进程或由 gofer 触发的空闲整理（用户决定只在 chat 进程内）。

## 已确认事实与规范

- 系统提示词由 `rulesPromptBuilder` 组装，顺序为 base、`personal_rules_content`、`memory_index_content`、`personal_append_prompt`、延迟工具列表（`internal/personal/runner.go:766-792`）；规则或附加提示变化时会话中途改写系统提示词并追加 system 通知（`runner.go:464-496`）。
- OpenAI 路径中 system 是 messages 首条、工具在 `body["tools"]`（`pkg/agentcore/llm/openai.go:97-100,158-170`）；Claude 路径把 role=system 的消息合并进顶层 `system`（`claude.go:113-122,163-176`）；两者都没有 `cache_control` / prompt cache 字段。
- 每个模型步骤都重新组装提示词、上下文并 `ListTools`（`pkg/agentcore/runtime.go:128-144`）。
- 压缩：阈值 = 窗口 − reserve；每步构建上下文时超阈值即在回合中途压缩（`context.go:162-193`）；摘要模板为“目标/约束/进展/当前计划/关键决策/涉及文件/下一步”（`compact.go:32-42`）；摘要作为首条 user 消息，保留切点之后的原始消息（`context.go:108-111,197-216`）；compaction 记录 `{summary, first_kept_seq, tokens_before, tokens_after}`；回合之间追加的手动 compaction 立即生效（`context.go:54-106`）；压缩后刷新记忆快照（`compact.go:226-229`）。
- 交接 `--from`：带摘要、计划、已加载工具到新会话，规则与记忆快照由新会话重新生成（`internal/personal/handoff.go`）。
- chat：回合同步执行、期间不进入等待输入（`internal/chat/chat.go:259-289`）；等待输入在 `nextChatInput` 中 select 键盘、投递、Stop hook 与 ctx（`chat.go:360-428`）；没有任何定时器；chat 终身持有会话锁，`Run`/`Compact` 在持锁进程内不再抢锁（`runner.go:204-236,334-337`、`compact.go:340-344`）。
- 评测：每个任务一次 `suag run --session-id`，会话固定，扩成多轮只需在同一会话多次调用（`scripts/eval/main.go:188`）。
- 同模型对比数据（2026-10-11，`tmp/eval/cmp-suag2`、`tmp/eval/cmp-omp`）：suag 8 任务 prompt 148k / 未命中 37k，omp 501k / 61k；小任务未命中 suag 3.5–4.8k、omp 0.8–2k。
- gofer 读取：`ndjson_usage_path: usage`，取最后一次出现的 usage 为当前总量；识别 `input_tokens`、`output_tokens`、`cache_read_tokens`、`cache_write_tokens`、`cost_usd`；ACP 读 session/update 的 `usage_update` 或 `_meta` 下的 usage。
- 适用规范：SR1107、SR1403、SR1405、SR1409、SR1402.1（真实边界验证）、GR104、GR106。

## 总体方案

请求的形状从“系统提示词里什么都有”改为三层：

```text
[系统提示词：稳定前缀]  身份、工作约定、skills 列表（与工作区、会话无关）
[工具定义：稳定前缀]    直接工具（会话中途只在 tool_search 加载时追加）
[段上下文消息]          本段冻结：环境、项目规则、记忆索引、附加提示、延迟工具列表、交接包
[段内历史]              只追加；规则/附加提示变化 → 追加一条上下文更新消息
```

断点（会话开始、达到阈值、手动 `:compact`、空闲整理、`--from`）时重算段上下文消息；断点之间只追加。这样：

- 跨会话、跨工作区复用的是前两层（约占小任务输入的大部分）；
- 段内任何变化都不改写前缀；
- 原来“在回合中途有损压缩”变为“在断点开启新段并带交接包”，旧段原文仍在底账里，可用 `session_search`/`recall` 取回。

## 架构

| 组件 | 改动 |
|---|---|
| `internal/wiring/prompt.go` | `basePrompt` 只保留稳定内容；环境一节移出，交给段上下文 |
| `internal/personal`（新 `segment.go`） | 段上下文消息的生成与冻结（session meta 存当前段的内容与 hash）；交接包生成；分段入口供阈值、`:compact`、空闲整理共用 |
| `internal/personal/context.go` | 消息序列：段上下文消息 → 交接包（属于段上下文的一部分）→ 段内历史 → 本回合；阈值处理改为调用分段入口 |
| `internal/personal/compact.go` | 摘要模板与输入改为面向当前任务（见“关键流程”）；compaction 记录扩展为段起始记录 |
| `internal/personal/runner.go` | `rulesPromptBuilder` 只输出稳定前缀；规则/附加提示变化改为追加上下文更新消息（复用 `EntryContextUpdate`） |
| `pkg/agentcore/llm/claude.go` | 在系统提示词末尾与工具定义末尾加 `cache_control` 断点（以 W0 验证为前提） |
| `internal/chat` | 空闲计时与后台分段、取消、状态行 |
| `internal/output/streamjson.go`、`internal/acpserver` | `turn_end` 累计 usage、`tool_calls`；核对 `usage_update` |
| `scripts/eval` | 多轮任务（`prompts` 列表）；“首次请求未命中缓存输入”指标 |

agentcore 内核不变：段上下文消息和交接包都是普通消息，由宿主的 `ContextBuilder` 产生；`cache_control` 只在 Claude provider 内部。

## 关键流程

**新会话 / 新段开始**

```text
断点触发 → 生成段上下文：环境、规则（重读）、记忆索引（重取）、附加提示、延迟工具列表
         → [若来自阈值/手动/空闲/--from] 生成交接包并并入段上下文
         → 段上下文内容与 hash 写入 session meta；写段起始记录（扩展 compaction 记录）
         → 之后每一步：稳定前缀 + 段上下文消息 + 段内历史 + 本回合
```

**交接包（面向当前任务）**：摘要调用的输入在现有基础上增加“当前任务”= 最后一条用户请求原文，以及候选 ref 清单（本段的外置输出 ref、`<sid>/<seq>` 消息 ref）。模板改为：

```text
## 当前任务（用户最后的请求，原文附在交接包里，不必复述）
## 目标与约束
## 进展（已完成 / 进行中 / 阻塞）
## 当前计划
## 关键决策
## 涉及文件
## 与当前任务相关的引用（ref + 一句话说明，可用 recall 读取）
## 下一步
```

交接包 = 摘要 + 最后一条用户请求原文 + 当前计划；段内保留的原始尾部缩小为“最后一组完整的工具调用/结果”（以现有切点规则为准），其余靠 ref 回读。

**段内规则/附加提示变化**：不改系统提示词，追加一条上下文更新消息（含新规则全文或附加提示），下一段起并入段上下文。

**空闲整理（chat）**

```text
回合结束 → 进入等待输入，启动计时器（context.idle_after，默认 15m）
计时到点 且 上下文估计 ≥ context.idle_min_ratio × 阈值（默认 0.5）且 会话未污染限制无关
  → 状态行“空闲整理中…” → 后台调用分段入口（compact_model）
  → 成功：写段起始记录，状态行“已整理上下文：X → Y token”
  → 期间有键盘输入或投递：取消（ctx），不写任何记录，照常处理输入
计时器在每次输入、每个回合开始时重置；进程退出即放弃
```

**W0 观测**：`turn_end` 事件携带 `usage`（本次 run 至此累计，含 `cache_read_tokens`、`cache_write_tokens`、`tool_calls`），`result` 行最后覆盖；评测从 `suag trace --json` 取每个会话首个模型调用的未命中输入。

## 安全、数据、运维与回滚

- 段上下文消息里的规则与记忆索引与现在系统提示词里的内容相同，只是位置变化；污染标记、记忆写入关口不受影响。
- 空闲整理只在 chat 持有会话锁的进程内运行，与回合互斥（回合开始前取消并等待其结束）；取消时不写记录，不会留下半段。
- 段起始记录在现有 compaction 记录上增加字段（`reason`、`context_hash`、`task`、`refs`）；旧记录字段缺省即按旧规则加载，不需迁移；回滚即恢复旧加载逻辑。
- `cache_control` 只影响计费与延迟，不影响语义；若 W0 验证显示 Claude 网关不支持或报错，保持不发。

## 决策

| ID | 决策 | 理由 |
|---|---|---|
| D1 | 系统提示词只放稳定内容，项目与会话相关内容放进段上下文消息 | 跨会话复用前缀；Codex 把环境与 AGENTS.md 放进首条 user 消息是同一思路 |
| D2 | 段内规则/附加提示变化改为追加上下文更新消息 | 断点之间只追加，不改写前缀 |
| D3 | 分段沿用现有 compaction 记录与切点规则，扩展字段而不是新表 | 加载逻辑与 `:compact` 已验证；最小改动（SR1403） |
| D4 | 交接包以“最后一条用户请求原文 + 计划 + 相关 ref”为核心，原始尾部缩到最后一组完整调用 | 面向当前任务；旧内容可经 ref 回读，不必留在上下文 |
| D5 | 空闲整理只在 chat 进程内计时，用户输入即取消 | 用户决定；无外部依赖；不与回合并发 |
| D6 | 空闲阈值与触发比例可配置，默认 15 分钟、上下文达阈值 50% | 空闲超过提供方缓存有效期后前缀已冷，整理几乎不额外损失缓存；上下文小的会话不值得整理 |
| D7 | Claude `cache_control` 先验证再加 | 现在完全没有断点，按 Anthropic 机制可能没有缓存；网关行为未知 |
| D8 | 观测字段与 gofer 约定一致：`turn_end` 累计 `usage`、`tool_calls` | gofer 取最后一次出现的值；中途预算可用 |

## 待确认事项

| ID | 问题 | 推荐 |
|---|---|---|
| Q1 | 空闲阈值默认值 | 15 分钟（可配 `context.idle_after`，`0` 关闭） |
| Q2 | 新段是否保留原始尾部 | 只保留最后一组完整的工具调用/结果；其余靠交接包与 ref |
| Q3 | 段内规则变化时追加的是全文还是“规则已变化”提示 | 追加全文（与系统提示词里原来的内容一致），下一段并入段上下文 |
| Q4 | `:compact` 的名字 | 保留 `:compact`（行为改为开启新段），不新增命令 |

## 结论与人工计划 Gate

本设计确立第二期范围（W0–W4）。批准后写实施计划，计划同样需要批准后才实施。批准语句示例：“批准设计 0.1，Q1–Q4 按推荐”。
