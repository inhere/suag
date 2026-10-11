<!-- template_id: plan; template_version: 1.2.0 -->
# 会话分段与上下文投影（第二期） 实施计划

> 状态：Draft 0.1 / 待人工计划批准

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-11 | Claude | 初稿 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

## 目标与完成定义

思考模式 RIGOROUS；核心目标：落地设计 0.3 的 W0–W4——观测与探针、稳定前缀、分段交接、定时组件与空闲整理、文档；scope freeze：设计 0.3 的范围、D1–D9、Q1–Q5 决议；review budget：一轮合并轴评审（低暴露度，SR1409）；停止条件：评审无 CORE_BLOCKING 且用户批准。

完成定义（对应设计“用户可见结果”1–4）：

1. 前缀缓存探针给出明确结论并记录；探针成立时，评测中小任务“首次请求未命中缓存的输入”降到当前（`tmp/eval/cmp-suag2`）的一半以下；不成立时按 T5 的降级方案执行并如实记录。
2. 达到阈值或 `:compact` 时写段起始记录、带交接包开新段；`--from` 的交接内容在后续分段后仍在；`suag trace` 可见分段与原因。
3. chat 空闲满 `context.idle_after` 且上下文达阈值 50% 以上时后台分段；输入、投递、Stop hook 回合、`:compact` 都能取消它且不留下未采纳的段。
4. stream-json 每次模型调用后输出累计用量（含缓存与 `tool_calls`）；ACP 中途发 `usage_update` 且区分 run / session；gofer-z2c6 已收到通知。
5. 通用验收：`go test -race ./...`、`go vet ./...`、`make smoke-chat`（含新空闲场景）通过；主机 Windows `go test ./...` 与 staticcheck 每个波次结束各一次且通过；分层守卫通过；单文件 ≤1000 行；每个改工具或请求形状的任务完成即在主机用真实模型跑一次最小提示。

## 范围、排除项与授权

运行目录：Git root `inhere-tools/suag`（容器路径 `/d/work/inhere/my-tools-dev/inhere-tools/suag`）；`AC = pkg/agentcore`。

范围：设计 0.3「范围」W0–W4。排除：设计非目标。

授权：本计划批准即授权按任务本地原子提交（SR1211），以及下列验证动作：经 `gofer job run -p my-tools-dev -a exec --runner local --cwd inhere-tools/suag --sync [--no-stall]` 在主机跑 `go test ./...`、staticcheck、`make install`、`go run ./scripts/eval`（真实模型 `opencode-go/deepseek-v4.1-flash`，隔离的临时 `SUAG_HOME`，强制 files 记忆后端）、前缀缓存探针与最小提示探测；向负责 gofer 的会话发送 W0 完成通知。不写真实 gofer memory；push 与发版不在授权内。

- `host_or_non_offline_action=REQUIRED`

## 输入与批准证据

- 设计：`docs/design/2026-10-11-segments-projection-design.md` Draft 0.3（b85498f）；用户原话：“批准设计 0.2”，Q1 补充“再加个到 ctx 50% 以上才触发（这里应该要加定时吧 - 定时/延迟功能对后续的自我触发等很重要，要封装好）；其他按推荐”。
- 设计评审：合并轴评审 NEEDS_REVISION（CB1–CB4）→ 0.2 全部处理；0.3 为用户补充的定时组件。
- 代码事实（2026-10-11 检索与评审核对）：系统提示词组装 `internal/personal/runner.go:766-792`；规则/附加提示更新 `runner.go:464-496`，一次性 system 通知 `runner.go:416`、`context.go:209-211`；`context_update` 不回放 `store/types.go:31-32`；skills 根 `internal/wiring/skills.go:37-43`；base 提示词 `internal/wiring/prompt.go`；压缩与切点 `internal/personal/compact.go:86-236`，采纳规则 `context.go:54-106`，compose `context.go:197-216`；交接 `handoff.go`；用量输出 `internal/output/streamjson.go:23-24`、`internal/output/usage.go:9-12`、`runner.go:736`；ACP 用量 `internal/acpserver/usage.go:17-27`；chat 回合与 `:compact` `internal/chat/chat.go:221-289`，等待输入 `chat.go:360-428`；Claude provider `pkg/agentcore/llm/claude.go:113-176`；评测运行 `scripts/eval/main.go`（单 prompt）。
- 基线：HEAD b85498f（实施前以 T0 实测为准）；对比数据 `tmp/eval/cmp-suag2`、`tmp/eval/cmp-omp`。

## Capability Discovery

### Capability decisions

| capability_id | required_capability | searched_candidates | direct_reuse | thin_adapter_or_owner_extension | decision | proven_gap | duplication_and_lifecycle_risk |
|---|---|---|---|---|---|---|---|
| CAP-01 | 运行中途累计用量 | `run_finished`/`result`、ACP `recordUsage`、personal `usageCollector` | 部分 | 在每次模型调用结束处输出累计（复用 usageCollector 与 ReportUsage） | OWNER_EXTENSION | 现在只在 run 结束输出 | 一处累计，多处输出 |
| CAP-02 | 多轮评测与首次未命中指标、前缀探针 | `scripts/eval`、`suag trace --json` | 是 | 扩展 task 格式与汇总 | OWNER_EXTENSION | none | 探针作为 eval 的一个模式，不另建脚本 |
| CAP-03 | 段起始记录与段上下文 | compaction 记录、handoff 记录、session meta 快照 | 部分 | 扩展 compaction 记录字段，加载逻辑统一 | OWNER_EXTENSION | 需要随回合采纳的冻结内容 | 不新建表；meta 只作缓存 |
| CAP-04 | 段内更新回放 | `EntryContextUpdate`（只审计） | 部分 | 改语义为回放并补写入方 | OWNER_EXTENSION | 现在不回放 | 改注释与测试，避免两种语义并存 |
| CAP-05 | 会话单飞 | `heldLocks`、`markRunning`、chat 会话锁 | 部分 | Runner 内按会话的单飞闸门 | OWNER_EXTENSION | 现有锁只防跨进程 | 唯一闸门供 Run/Compact/分段 |
| CAP-06 | 通用定时组件 | `time.AfterFunc`、chat 的 exitConfirmWindow、gofer schedule（外部） | 否 | 新建 `AC/schedule`（NEW-01） | MINIMAL_NEW_MODULE | 用户要求封装好、供后续自我触发复用；chat 内无定时器 | 只依赖标准库；后续 people 复用 |
| CAP-07 | Claude 缓存断点 | `claude.go` 请求构建 | 是 | 原位扩展 system block 与 `cache_control` | OWNER_EXTENSION | none | 只在 Claude provider 内 |

### New module candidates

| candidate_id | capability_id | proposed_module | searched_candidates | direct_reuse_gap | thin_adapter_or_owner_extension_gap | proven_gap | unique_owner_and_lifecycle | deletion_or_merge_handling |
|---|---|---|---|---|---|---|---|---|
| NEW-01 | CAP-06 | `AC/schedule` | `time.AfterFunc`、chat 内计时、gofer schedule | AfterFunc 无命名任务、取消重置、空闲语义、事件与假时钟 | 写在 chat 里无法给后续自我触发复用 | 用户明确要求封装；第二个使用者（people 第二循环）已在路线上 | AC 能力层，宿主注入事件回调 | 若长期只有 chat 一个使用者，可内联回 chat |

### Rejected new tools

| rejected_candidate | capability_id | deletion_test_and_reason |
|---|---|---|
| 新 segments 表 | CAP-03 | 删掉它无损：compaction 记录扩展字段即可，加载逻辑与采纳规则已验证 |
| 独立的前缀探针脚本 | CAP-02 | 删掉它无损：作为 eval 的一个模式即可复用隔离与汇总 |

## 前置检查与 fail-closed 条件

- workspace baseline：Git root `inhere-tools/suag`，branch `main`，HEAD 以 T0 实测为准；`git status --short` 中非本计划的改动全程不暂存、不修改（Ownership Conflict 时停止）；Go 1.26；主机 gofer 可用。
- 测试隔离：测试不触碰真实 `~/.suag`；全量 `go test` 保持默认 TMPDIR；评测与探针用临时 `SUAG_HOME`、强制 files 记忆后端。
- 每个任务：`gofmt -l .` 为空 → `go build ./... && go vet ./...` → `go test -race ./... > tmp/sp-tN.log 2>&1; echo $?` 为 0 → 分层守卫 → 提交；终端交互改动跑 `make smoke-chat`；每个波次结束在主机跑 `go test ./...` 与 staticcheck。
- 改动工具定义、提示词组装或请求形状的任务（T1、T4、T5、T6、T7），完成即在主机用真实模型跑最小提示（隔离 SUAG_HOME），不得等到 T12。
- fail-closed：全量回归即停止修复；分层守卫失败或 import cycle 即停止并回到设计；实施中发现的新路径按计划合同四类分类记录，Semantic Amendment 停止并回到计划 Gate。
- 委派子 agent 时，说明里带上 gofer memory `subagent-impl-brief-rules` 的全部要点。

## 波次与依赖

| 波次 | 任务 | 依赖 |
|---|---|---|
| 0 | T0 基线 | — |
| 1 W0 | T1 运行中途用量 → T2 评测多轮与首次未命中指标、探针模式 → T3 主机探针与基线 | T0 |
| 2 W1/W2 | T4 Claude 断点（验证后启用）→ T5 段起始记录与段上下文（含提示词搬移，按 T3 结论）→ T6 段内更新回放 → T7 分段交接 → T8 会话单飞 | T3 |
| 3 W3 | T9 `AC/schedule` → T10 chat 空闲整理 | T8 |
| 4 W4 | T11 文档 → T12 全量与真实验证 | T10 |

单 Agent 顺序执行；进度记在新建的 gofer plan（批准执行时创建，含 T0–T12 全部 todo）。

## 任务

### T0 基线

- 文件: 无
- 动作: 记录 HEAD 与 `git status --short`；跑全量测试与冒烟。
- 验证: `go test -race ./... > tmp/sp-t0.log 2>&1; echo $?`；`make smoke-chat`。
- 完成标准: 全绿，记入 gofer plan。
- 依赖: —

### T1 运行中途累计用量（W0、D8）

- 文件: `internal/personal`（usageCollector 与模型调用结束处的回调）、`internal/output/streamjson.go`、`internal/output/usage.go`、`internal/acpserver/usage.go`、`internal/acpserver/server.go`，以及各自测试
- 动作: 每次模型调用结束输出一行 `{"type":"usage","usage":{...}}`（本次 run 至此累计：`input_tokens`、`output_tokens`、`cache_read_tokens`、`cache_write_tokens`、`total_tokens`、`cost_usd`、`tool_calls`）；`turn_end` 与 `result` 带同名 `usage`；ACP 在同一时机发 `usage_update`，`_meta` 给出 `run` 与 `session` 两组累计；`tool_calls` 为本次 run 的工具调用次数（含被拒）。
- 验证: 全量测试；stream-json 测试断言多次模型调用时 usage 单调递增且 result 与最后一次一致；ACP 测试断言中途收到 usage_update 且 run/session 区分；主机最小提示：`suag run --output-format stream-json` 的 stderr 含 usage 行。
- 完成标准: 测试通过；主机实测字段齐全。
- 依赖: T0

### T2 评测：多轮任务、首次未命中指标与探针模式（W0、D7）

- 文件: `scripts/eval/main.go`、`scripts/eval/task.go`、`scripts/eval/omp.go`、`scripts/eval/README.md`、新增 1–2 个多轮任务（`testdata/tasks/<name>`）
- 动作: `task.yaml` 支持 `prompts` 列表（同一会话依次 `suag run --session-id`，omp 用 `--continue`/会话目录等价方式，做不到则 omp 只跑单轮并在结果注明）；`answer_contains` 判定最后一轮；新增指标 `first_call_uncached`（suag：`suag trace --json` 中首个成功的主模型调用 `input − cache_read − cache_write`，排除失败重试与 compact 调用；omp：首个 assistant `message_end` 的 `input`）；新增 `--probe-prefix` 模式：同一模式下依次在两个不同工作区各跑一次最小提示，输出第二次首个调用的 `cache_read` 与未命中量。
- 验证: `go vet ./scripts/...`；假模型跑多轮任务与探针模式退出 0。
- 完成标准: results.jsonl 含新字段；探针模式输出两次调用的对比。
- 依赖: T1

### T3 主机探针与基线（W0）

- 文件: 无（结果记入 gofer plan 与 `tmp/eval/`）
- 动作: 主机用当前版本跑探针模式与全量评测（含多轮任务），记录 `first_call_uncached` 基线；判定“system + tools 前缀可跨工作区缓存”是否成立（第二次首调 `cache_read` 覆盖到 system+tools 的大部分）。
- 验证: 主机 eval 退出 0；结论与数字写入 gofer plan 的 T3 备注。
- 完成标准: 有明确结论（成立 / 不成立 / 无法判定及原因）。
- 依赖: T2

### T4 Claude 缓存断点（W1、D7）

- 文件: `pkg/agentcore/llm/claude.go`(+test)
- 动作: `system` 改为 block 数组；在段上下文消息（T5 前为首条 user 消息）末尾与最后一条消息上加 `cache_control: {type: ephemeral}`；提供开关（provider 选项，默认开），网关拒绝时可关。
- 验证: 单测断言请求体形状；主机若配置有 anthropic 类型的 provider，用它跑一次两轮对话，第二轮 `cache_read > 0`；没有可用的 anthropic 路由时记 `UNVERIFIED_REAL_ENV` 并保持默认关。
- 完成标准: 测试通过；真实验证结论记录。
- 依赖: T3

### T5 段起始记录与段上下文（W1/W2、D1、D2）

- 文件: 新 `internal/personal/segment.go`(+test)、`internal/personal/context.go`、`internal/personal/compact.go`（记录结构）、`internal/personal/runner.go`、`internal/personal/memory.go`、`internal/wiring/prompt.go`、`internal/wiring/runtime.go`（注入 `EnvironmentSource`、`SkillsSource`）
- 动作: 段起始记录 = compaction 记录 + `reason`（start/threshold/manual/idle/handoff）、`context`（环境、规则、记忆索引、skills 列表、附加提示、延迟工具列表的快照与 hash）、`handoff`、`recent_requests`、`refs`；新会话写一条 start 记录；`loadHistory` 以最新已采纳的段记录确定段上下文（旧会话无记录时用 meta 快照兼容渲染，旧 `handoff` 视为第 0 段交接包）；`compose` 生成段上下文消息（user 角色，不是 ctxItem，不参与切点、不进 `items()`）；meta 只作缓存；删除 `promptStale`/`memoryPromptHook` 与 `contextBuilder.meta` 共享。按 T3 结论：成立 → `basePrompt` 只留身份与工作约定，其余进段上下文；不成立 → 规则/记忆/环境/skills 仍留在系统提示词，段上下文消息只承载交接包（降级，记 Operational Discovery）。
- 验证: 全量测试；新测试：段记录随回合采纳（回合失败时不生效）、旧会话兼容、`--from` 内容在第二次分段后仍可见、段上下文消息不被切点选中、两工作区同模式下系统提示词字节相同（成立时）；`make smoke-chat`；主机最小提示。
- 完成标准: 测试与冒烟通过；主机实测正常。
- 依赖: T4

### T6 段内更新回放（W1、D3）

- 文件: `internal/personal/runner.go`、`internal/personal/context.go`、`internal/personal/store/types.go`（注释）、`internal/personal/compact.go`（摘要输入跳过）、hook `SystemContext` 与 lostReply 的生成处(+tests)
- 动作: 回合开始比较生效 hash（段记录 hash，被已采纳的最新 `context_update` 覆盖）与当前规则/附加提示 hash，不同则写 `context_update`（user 角色全文，随回合采纳）；`loadHistory` 回放已采纳的 `context_update`；不进 `state.Messages`；摘要输入跳过它；hook `SystemContext` 与 lostReply 改为 user 角色；`personal_rules_hash` 等 meta 键改为缓存语义。
- 验证: 全量测试；新测试：规则变化后下一回合与再下一回合都看到新规则且只追加一次、回合失败后重判不丢、Claude 路径下 system 字段在段内不变；主机最小提示。
- 完成标准: 测试通过。
- 依赖: T5

### T7 分段交接（W2、D4）

- 文件: `internal/personal/compact.go`、`internal/personal/segment.go`、`internal/personal/handoff.go`、`internal/personal/trace.go`(+tests)
- 动作: 阈值与 `:compact` 改为调用分段入口：生成交接包（模板见设计；`previous` = 当前段交接包；输入跳过 `context_update`；附最近 3 条用户请求原文，跳过 Stop hook 注入与“继续”类短句；候选 ref 来自本段外置输出与消息 ref）；写段记录（reason=threshold/manual），段内保留最后一组完整调用/结果；trace 显示分段（原因、前后 token、段号）。
- 验证: 全量测试；新测试：交接包含最近请求原文与 ref、`--from` 后再分段仍保留交接内容、阈值分段后模型请求中段上下文与交接包正确；评测多轮任务在小窗口配置下触发分段并通过；主机最小提示。
- 完成标准: 测试通过；trace 可见。
- 依赖: T6

### T8 会话单飞（W2/W3、D5）

- 文件: `internal/personal/runner.go`、`internal/personal/compact.go`、`internal/personal/segment.go`(+tests)
- 动作: Runner 按会话的单飞闸门（Run、Compact、分段入口共用；带 ctx 等待与 try 获取）；段记录为唯一提交点：提交前取消 → compaction 事件 status=cancelled，不写记录与 meta；提交后取消 → 视为成功；`fail()` 对取消不再记 error。
- 验证: 全量测试（含 -race）；并发测试：分段进行中发起 Run/Compact 会先取消再等待、不产生两条段记录。
- 完成标准: 测试通过。
- 依赖: T7

### T9 定时组件 `AC/schedule`（W3、D9）

- 文件: 新 `pkg/agentcore/schedule/schedule.go`(+test)
- 动作: 实现设计中的接口：`After`、`Every`、`Idle`、`Touch`、`Close`、`Handle{Cancel, Reset}`；同名替换；同一任务不并发（上次未结束则跳过并记 skipped）；选项：时钟注入（假时钟用于测试，不睡眠）、`Observe` 事件回调（scheduled/fired/skipped/cancelled/done/failed，带名字与耗时）；只依赖标准库。
- 验证: 全量测试（-race）；假时钟覆盖：延迟触发、周期、Idle 被 Touch 推迟、取消与重置、Close 等待运行中任务、同名替换、跳过并发；分层守卫。
- 完成标准: 测试通过；反向验证（去掉 Touch 重置后 Idle 测试失败）。
- 依赖: T8

### T10 chat 空闲整理（W3、D5、D6）

- 文件: `internal/chat/chat.go`、`internal/chat/render.go`、`internal/config`（`context.idle_after`、`context.idle_min_ratio`）、`scripts/ptysmoke/`（新场景）(+tests)
- 动作: chat 创建 Scheduler，注册 `Idle("idle-consolidate", idle_after, fn)`；fn 检查上下文估计 ≥ `idle_min_ratio` × 阈值后 try 获取单飞并调用分段入口（reason=idle），状态行“空闲整理中…”/“已整理上下文：X → Y token”；键盘输入、投递、Stop hook 回合、`:compact`、回合开始先 `Touch()` 并取消进行中的整理再等待；事件经 Observe 写入 events；`idle_after: 0` 关闭。
- 验证: 全量测试；假时钟/短阈值测试覆盖触发、比例不足不触发、输入取消且不留记录；`make smoke-chat` 新场景（短阈值配置下空闲后出现整理提示、随后输入正常）。
- 完成标准: 测试与冒烟通过。
- 依赖: T9

### T11 文档（W4）

- 文件: `README.md`、`README.zh-CN.md`、`pkg/agentcore/README.md`（schedule）、`config/config.yaml`（`context.*`）、`docs/usage/gofer-personal-agent.md`（用量行、ACP 用量）、`CHANGELOG.md` 未发布条目
- 动作: 按代码更新；列出与设计的差异。
- 验证: 文档中的命令在临时 SUAG_HOME 下可执行。
- 完成标准: 文档与实现一致。
- 依赖: T10

### T12 全量与真实验证

- 文件: 无（问题回到对应任务）
- 动作: 容器全量 race、vet、`make smoke-chat`；主机 `go test ./...` 与 staticcheck；主机确认 `suag ps` 为空后备份 `sessions.db`（连同 `-wal`/`-shm`）再 `make install`；主机评测（含多轮）与 T3 基线对比 `first_call_uncached`、prompt 总量、耗时；用 omp 同任务对比；通知负责 gofer 的会话 W0 已落地（gofer-z2c6）。
- 验证: 结果写入 gofer plan；数字对比表。
- 完成标准: 完成定义 1–5 满足。
- 依赖: T11

## 回滚与恢复

- 每个任务独立提交，可按任务 `git revert`；T5–T8 有依赖，回滚时从后往前。
- 数据：段记录只扩展 compaction 记录字段；`context_update` 开始回放只影响新条目；旧记录按旧规则加载。主机安装前有 `sessions.db` 备份。
- 主机安装回滚：按上一个提交重新 `make install`。
- 中断恢复：按 gofer plan 最后完成的任务与 HEAD 继续（SR1407）。

## 人工 Gate

- 计划批准（本文件）。
- 当前执行请求：批准后另需一句明确的开始执行。
- T3 结论若为“不成立”，T5 按降级方案执行并在汇报中说明（设计 D7 已预先约定，不另行请求批准）。
- 外部动作 Gate：push 与发版不在本计划授权内。

## 可追溯性

| 设计条目 | 任务 | 验证 |
|---|---|---|
| D1 稳定前缀 | T3、T5 | 探针结论；两工作区系统提示词字节相同；评测 first_call_uncached |
| D2 段记录为准、meta 只作缓存 | T5 | 回合失败不生效、旧会话兼容测试 |
| D3 段内更新回放 | T6 | 只追加一次、失败重判测试 |
| D4 交接包 | T7 | 最近请求与 ref、`--from` 后再分段测试 |
| D5 会话单飞与提交点 | T8、T10 | 并发与取消测试 |
| D6 空闲整理条件 | T10 | 比例不足不触发、冒烟 |
| D7 探针门槛与 Claude 断点 | T2、T3、T4 | 探针输出、请求体测试、真实验证或 UNVERIFIED |
| D8 用量输出 | T1 | 单调递增、ACP run/session 测试、主机实测 |
| D9 定时组件 | T9、T10 | 假时钟测试、反向验证 |
| Q1–Q5 | T7、T10、T6 | 对应测试 |

## 完成 Gate 与剩余工作

完成条件：完成定义 1–5 满足，T12 记录齐全，gofer plan 全部 todo 完成。完成后提出：push 与是否发版；suag-jz0l（循环核心对比）；空闲时提炼记忆候选；people（suag-frgt）。
