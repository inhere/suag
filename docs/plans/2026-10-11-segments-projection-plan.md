<!-- template_id: plan; template_version: 1.2.0 -->
# 会话分段与上下文投影（第二期） 实施计划

> 状态：Draft 0.2 / 待人工计划批准

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-11 | Claude | 初稿 |
| 0.2 | 2026-10-11 | Claude | 按合并评审修订：探针分“字节相同 / 跨工作区 / 随机前缀对照”三组并重复，判定只看前后两组对比（CB1）；ACP `_meta` 保留扁平键（= run 口径）并加 run/session 嵌套，写明 run 计入 compact 与失败尝试，用 gofer 解析方式做 fixture，T1 完成即通知 gofer（CB2）；T5 拆为段记录（提示词不动）与“提示词搬移 + 段内更新”两步，写明降级时系统提示词来源与回合中途重建（CB3）；单飞闸门支持可抢占持有者，空闲计时以回合结束为起点、回合中暂停（CB4）；段记录的 `summary` 写入完整交接包、旧结构可解码（CB5）；段上下文消息用 Metadata 标记，Claude 断点移到段记录之后；compact 调用的 model 事件加 purpose；主机探测用本次构建的二进制；预先拆分 runner.go；T9 可派给 suag/omp/codex 同任务对比 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

## 目标与完成定义

思考模式 RIGOROUS；核心目标：落地设计 0.3 的 W0–W4——观测与探针、稳定前缀、分段交接、定时组件与空闲整理、文档；scope freeze：设计 0.3 的范围、D1–D9、Q1–Q5 决议；review budget：一轮合并轴评审（低暴露度，SR1409，已完成）；停止条件：用户批准。

完成定义（对应设计“用户可见结果”1–4）：

1. 前缀缓存探针给出基于对照组的明确结论并记录；成立时，评测中小任务 `first_call_uncached` 降到 `tmp/eval/cmp-suag2` 基线的一半以下；不成立时按 T6 的降级方案执行并如实记录。
2. 达到阈值或 `:compact` 时写段起始记录、带交接包开新段；`--from` 的交接内容在后续分段后仍在；用旧结构解码新段记录不丢上下文；`suag trace` 可见分段与原因。
3. chat 空闲满 `context.idle_after`（从回合结束起算）且上下文达阈值 50% 以上时后台分段；回合中不触发；输入、投递、Stop hook 回合、`:compact` 都能抢占它且不留下未采纳的段。
4. stream-json（stdout）每次模型调用后输出累计用量（含缓存与 `tool_calls`）；ACP 中途发 `usage_update`，`_meta` 扁平键为 run 口径并附 run/session 嵌套；gofer 用其现有解析读取正确；已通知 gofer（gofer-z2c6）并得到确认。
5. 通用验收：`go test -race ./...`、`go vet ./...`、`make smoke-chat`（含新空闲场景）通过；主机 Windows `go test ./...` 与 staticcheck 每个波次结束各一次且通过；分层守卫通过；单文件 ≤1000 行；改动工具、提示词组装或请求形状的任务完成即在主机用本次构建的二进制跑一次真实模型最小提示。

## 范围、排除项与授权

运行目录：Git root `inhere-tools/suag`（容器路径 `/d/work/inhere/my-tools-dev/inhere-tools/suag`）；`AC = pkg/agentcore`。

范围：设计 0.3「范围」W0–W4。排除：设计非目标。

授权：本计划批准即授权按任务本地原子提交（SR1211），以及下列验证动作：经 `gofer job run -p my-tools-dev -a exec --runner local --cwd inhere-tools/suag --sync [--no-stall]` 在主机跑 `go test ./...`、staticcheck、`go build`、`make install`、`go run ./scripts/eval`（真实模型 `opencode-go/deepseek-v4.1-flash`，隔离的临时 `SUAG_HOME`，强制 files 记忆后端；omp 多轮用临时 `--session-dir`）、前缀缓存探针与最小提示探测；向负责 gofer 的会话发送 T1 完成通知；T9 按用户选定方式派给 suag/omp/codex（各自独立 worktree，不合并、不推送，由我复核后择一并入）。不写真实 gofer memory；push 与发版不在授权内。

- `host_or_non_offline_action=REQUIRED`

## 输入与批准证据

- 设计：`docs/design/2026-10-11-segments-projection-design.md` Draft 0.3（b85498f）；用户原话：“批准设计 0.2”，Q1 补充“再加个到 ctx 50% 以上才触发（这里应该要加定时吧 - 定时/延迟功能对后续的自我触发等很重要，要封装好）；其他按推荐”。
- 设计评审：合并轴评审 NEEDS_REVISION（CB1–CB4）→ 0.2 处理；0.3 为用户补充的定时组件。计划评审：合并轴评审 NEEDS_REVISION（CB1–CB5）→ 本 0.2 处理并采纳 5 条建议。
- 代码事实（检索与两轮评审核对）：系统提示词组装 `internal/personal/runner.go:766-792`（runner.go 现 792 行）；规则/附加提示更新 `runner.go:464-502`，一次性 system 通知 `runner.go:416`、`context.go:210`，“以 system prompt 中的版本为准”提示 `runner.go:479`；`context_update` 不回放 `store/types.go:31-32`；skills 根 `internal/wiring/skills.go:37-43`；base 提示词环境段 `internal/wiring/prompt.go:55-57`；agentcore 每步先 buildPrompt 后 buildContext `pkg/agentcore/runtime.go:295,318`；压缩与切点 `internal/personal/compact.go:86-236`，compact 调用走 `compactProvider().Generate`（`compact.go:269`，未配 compact_model 时与主模型同一 provider，`compact.go:243-246`），写记录用 `WithoutCancel`（`compact.go:222`）；旧 `compactionRecord` 只读 `summary`/`first_kept_seq`（`context.go:42-47,72-78`）；stream-json 写 stdout（`internal/cli/run.go:146-148`）；ACP `_meta` 现为会话累计（`internal/acpserver/usage.go:30-50`）；gofer 读 ACP 用量只取 `_meta` 顶层扁平键（gofer `internal/runner/acp/usage.go:21-41`、`internal/runner/usage.go:62-68`）；chat 输出写入经 `lockedWriter`（`internal/chat/chat_input.go:436`）；`Message.Metadata` 可用于标记（`pkg/agentcore/api/types.go:40`）。
- 基线：HEAD 以 T0 实测为准；对比数据 `tmp/eval/cmp-suag2`、`tmp/eval/cmp-omp`。

## Capability Discovery

### Capability decisions

| capability_id | required_capability | searched_candidates | direct_reuse | thin_adapter_or_owner_extension | decision | proven_gap | duplication_and_lifecycle_risk |
|---|---|---|---|---|---|---|---|
| CAP-01 | 运行中途累计用量 | `run_finished`/`result`、ACP `recordUsage`、personal `usageCollector`、observe 的 model 事件 | 部分 | 以 observe 层（覆盖 compact 与失败尝试）累计 run 口径，输出到 stream-json 与 ACP | OWNER_EXTENSION | 现在只在 run 结束输出，且不含 compact | 一处累计，多处输出 |
| CAP-02 | 多轮评测、首次未命中指标、前缀探针 | `scripts/eval`、`suag trace --json` | 是 | 扩展 task 格式、汇总与探针模式；model 事件加 purpose | OWNER_EXTENSION | none | 探针作为 eval 的模式，不另建脚本 |
| CAP-03 | 段起始记录与段上下文 | compaction 记录、handoff 记录、session meta 快照 | 部分 | 扩展 compaction 记录字段（`summary` 写完整交接包），加载逻辑统一 | OWNER_EXTENSION | 需要随回合采纳的冻结内容 | 不新建表；meta 只作缓存；旧二进制可解码 |
| CAP-04 | 段内更新回放 | `EntryContextUpdate`（只审计） | 部分 | 改语义为回放并补写入方 | OWNER_EXTENSION | 现在不回放 | 改注释与测试，避免两种语义并存 |
| CAP-05 | 会话单飞（可抢占） | `heldLocks`、`markRunning`、chat 会话锁 | 部分 | Runner 内按会话的闸门，支持可抢占持有者 | OWNER_EXTENSION | 现有锁只防跨进程 | 唯一闸门供 Run/Compact/分段 |
| CAP-06 | 通用定时组件 | `time.AfterFunc`、chat 的 exitConfirmWindow、gofer schedule（外部） | 否 | 新建 `AC/schedule`（NEW-01） | MINIMAL_NEW_MODULE | 用户要求封装好、供后续自我触发复用；chat 内无定时器 | 只依赖标准库；后续 people 复用 |
| CAP-07 | Claude 缓存断点 | `claude.go` 请求构建 | 是 | 原位扩展 system block 与 `cache_control`，以 Metadata 标记定位 | OWNER_EXTENSION | none | 只在 Claude provider 内 |

### New module candidates

| candidate_id | capability_id | proposed_module | searched_candidates | direct_reuse_gap | thin_adapter_or_owner_extension_gap | proven_gap | unique_owner_and_lifecycle | deletion_or_merge_handling |
|---|---|---|---|---|---|---|---|---|
| NEW-01 | CAP-06 | `AC/schedule` | `time.AfterFunc`、chat 内计时、gofer schedule | AfterFunc 无命名任务、取消重置、暂停、空闲语义、事件与假时钟 | 写在 chat 里无法给后续自我触发复用 | 用户明确要求封装；第二个使用者（people 第二循环）已在路线上 | AC 能力层，宿主注入事件回调 | 若长期只有 chat 一个使用者，可内联回 chat |

### Rejected new tools

| rejected_candidate | capability_id | deletion_test_and_reason |
|---|---|---|
| 新 segments 表 | CAP-03 | 删掉它无损：compaction 记录扩展字段即可，加载逻辑与采纳规则已验证 |
| 独立的前缀探针脚本 | CAP-02 | 删掉它无损：作为 eval 的模式即可复用隔离与汇总 |

## 前置检查与 fail-closed 条件

- workspace baseline：Git root `inhere-tools/suag`，branch `main`，HEAD 以 T0 实测为准；`git status --short` 中非本计划的改动全程不暂存、不修改（Ownership Conflict 时停止）；Go 1.26；主机 gofer 可用。
- 测试隔离：测试不触碰真实 `~/.suag`；全量 `go test` 保持默认 TMPDIR；评测与探针用临时 `SUAG_HOME`、强制 files 记忆后端。
- 每个任务：`gofmt -l .` 为空 → `go build ./... && go vet ./...` → `go test -race ./... > tmp/sp-tN.log 2>&1; echo $?` 为 0 → 分层守卫 → 提交；终端交互改动跑 `make smoke-chat`；每个波次结束在主机跑 `go test ./...` 与 staticcheck。
- 主机真实模型探测一律先 `go build -o tmp/probe/suag.exe ./cmd/suag`（主机），用它而不是已安装的 `suag.exe`；评测用 `--bin` 指向它。
- 改动工具定义、提示词组装或请求形状的任务（T1、T4、T5、T6、T7）完成即做一次主机最小提示（隔离 SUAG_HOME），T6 另确认连续多条 user 消息被网关接受。
- fail-closed：全量回归即停止修复；分层守卫失败或 import cycle 即停止并回到设计；实施中发现的新路径按计划合同四类分类记录，Semantic Amendment 停止并回到计划 Gate。
- 委派子 agent 时，说明里带上 gofer memory `subagent-impl-brief-rules` 的全部要点。

## 波次与依赖

| 波次 | 任务 | 依赖 |
|---|---|---|
| 0 | T0 基线（含预先拆分 runner.go） | — |
| 1 W0 | T1 运行中途用量（完成即通知 gofer）→ T2 评测多轮、首次未命中指标、三组探针 → T3 主机探针与基线 | T0 |
| 2 W1/W2 | T4 段起始记录与段上下文（提示词不动）→ T5 Claude 断点 → T6 提示词搬移与段内更新（按 T3 结论）→ T7 分段交接 → T8 会话单飞（可抢占） | T3 |
| 3 W3 | T9 `AC/schedule`（可与 T4–T8 并行，可派给 suag/omp/codex 对比）→ T10 chat 空闲整理 | T9 需 T0；T10 需 T8、T9 |
| 4 W4 | T11 文档 → T12 全量与真实验证 | T10 |

单 Agent 顺序执行主线；T9 按用户选定的方式派出。进度记在新建的 gofer plan（批准执行时创建，含 T0–T12 全部 todo）。

## 任务

### T0 基线与预拆分

- 文件: `internal/personal/runner.go` → 拆出 `internal/personal/prompt_builder.go`（`rulesPromptBuilder` 及提示词相关）与 `internal/personal/gate.go`（预留单飞闸门，先放现有锁相关函数）
- 动作: 记录 HEAD 与 `git status --short`；纯移动拆分 runner.go（不改行为）；跑全量测试与冒烟。
- 验证: `go test -race ./... > tmp/sp-t0.log 2>&1; echo $?`；`make smoke-chat`；拆分前后测试名单一致。
- 完成标准: 全绿；runner.go 明显低于 1000 行。
- 依赖: —

### T1 运行中途累计用量（W0、D8）

- 文件: `internal/personal/observe.go`、`internal/personal/usage.go`、`internal/output/streamjson.go`、`internal/output/usage.go`、`internal/acpserver/usage.go`、`internal/acpserver/server.go`(+tests)
- 动作: run 口径累计在 observe 层：本次 run 内所有模型调用（含 compact 调用与报告了用量的失败尝试，不含被取消且无用量的调用），`tool_calls` 为本次 run 的工具调用次数（含被拒）；每次模型调用结束输出一行 `{"type":"usage","usage":{input_tokens,output_tokens,cache_read_tokens,cache_write_tokens,total_tokens,cost_usd,tool_calls}}` 到 stream-json（stdout）；`turn_end` 与 `result` 带同名 `usage`（`result` 为最终值）；ACP 在同一时机发 `usage_update`，`_meta` 顶层扁平键为 run 口径，另附 `run`、`session` 两个嵌套对象。完成后向负责 gofer 的会话发送字段说明并请其确认（gofer-z2c6）。
- 验证: 全量测试；stream-json 测试：多次模型调用（含一次 compact）时 usage 单调递增、result 与最后一次一致；ACP 测试：中途收到 usage_update，扁平键 = run；fixture 测试按 gofer `UsageFromObject` 的扁平键规则解析 `_meta`，得到 run 值；主机最小提示：stdout 含 usage 行。
- 完成标准: 测试通过；主机实测字段齐全；gofer 侧确认读取正确。
- 依赖: T0

### T2 评测：多轮、首次未命中指标与三组探针（W0、D7）

- 文件: `scripts/eval/main.go`、`scripts/eval/task.go`、`scripts/eval/omp.go`、`scripts/eval/README.md`、新增 1–2 个多轮任务；`internal/personal/compact.go` 与 observe（model 事件加 `purpose: "compact"`）；`internal/wiring/prompt.go`（仅读取隐藏环境变量 `SUAG_DEBUG_PROMPT_NONCE`，非空时放在系统提示词最前，供探针对照组使用）(+tests)
- 动作: `task.yaml` 支持 `prompts` 列表（suag 同一会话依次 `run`；omp 用临时 `--session-dir` + `--continue`）；`answer_contains` 判定最后一轮；指标 `first_call_uncached`（suag：trace 中首个成功且 purpose≠compact 的主模型调用 `input − cache_read − cache_write`；omp：首个 assistant `message_end` 的 `input`）；`--probe-prefix` 模式输出三组，每组重复 ≥3 次、两次请求间隔数秒：(a) 字节相同组——同一工作区路径、两个新会话、新 SUAG_HOME；(b) 跨工作区组——两个不同工作区；(c) 对照组——同 (a) 但设置随机 nonce；同时输出 system+tools 的 token 估计与 cache_read 覆盖率。
- 验证: `go vet ./scripts/...`；假模型跑多轮任务与探针模式退出 0；单测：nonce 只在环境变量设置时出现。
- 完成标准: results.jsonl 含新字段；探针输出三组对比表。
- 依赖: T1

### T3 主机探针与基线（W0）

- 文件: 无（结果记入 gofer plan 与 `tmp/eval/`）
- 动作: 主机用本次构建的二进制跑探针与全量评测（含多轮），记录 `first_call_uncached` 基线；判定：(a) 组 cache_read 覆盖 system+tools 的大部分且显著高于 (c) 组 → 成立；(a) 与 (c) 无差别 → 不成立；波动过大 → 无法判定（按不成立处理并注明）。
- 验证: 主机 eval 退出 0；结论与数字写入 gofer plan 的 T3 备注。
- 完成标准: 有基于对照组的明确结论。
- 依赖: T2

### T4 段起始记录与段上下文（W2、D2、D4 的数据部分）

- 文件: 新 `internal/personal/segment.go`(+test)、`internal/personal/context.go`、`internal/personal/compact.go`（记录结构）、`internal/personal/runner.go`(新建会话写 start 记录)、`internal/personal/handoff.go`
- 动作: 段起始记录 = compaction 记录 + `reason`（start/threshold/manual/idle/handoff）、`context`（环境、规则、记忆索引、skills 列表、附加提示、延迟工具列表的快照与 hash）、`recent_requests`、`refs`；`summary` 始终写入渲染后的完整交接包（start 记录为空），`first_kept_seq` 语义不变；新会话写一条 start 记录；`loadHistory` 以最新已采纳的段记录确定段上下文（旧会话无记录时用 meta 快照兼容；旧 `handoff` 视为第 0 段交接包）；`compose` 生成段上下文消息（user 角色，`Metadata["suag.segment_context"]=true`，不是 ctxItem，不参与切点、不进 `items()`）。本任务不搬移系统提示词内容，`promptStale`/`memoryPromptHook` 保留；段上下文消息此时只承载交接包（若有）。
- 验证: 全量测试；新测试：段记录随回合采纳（回合失败不生效）、旧会话兼容、`--from` 内容在第二次分段后仍可见、段上下文消息不被切点选中、用旧 `compactionRecord` 结构解码新记录得到完整交接包；`make smoke-chat`；主机最小提示。
- 完成标准: 测试与冒烟通过；主机实测正常。
- 依赖: T3

### T5 Claude 缓存断点（W1、D7）

- 文件: `pkg/agentcore/llm/claude.go`(+test)
- 动作: `system` 改为 block 数组；在带 `suag.segment_context` 标记的消息末尾与最后一条消息上加 `cache_control: {type: ephemeral}`；provider 选项开关，默认关；主机验证通过后在 wiring 对 anthropic 类型 provider 默认打开。
- 验证: 单测断言请求体形状与标记定位；主机若配置有 anthropic 类型的 provider，用它跑两轮对话，第二轮 `cache_read > 0` 后打开默认；没有可用路由时保持默认关并记 `UNVERIFIED_REAL_ENV`。
- 完成标准: 测试通过；真实验证结论记录。
- 依赖: T4

### T6 提示词搬移与段内更新（W1、D1、D3）

- 文件: `internal/personal/prompt_builder.go`、`internal/personal/runner.go`、`internal/personal/context.go`、`internal/personal/segment.go`、`internal/personal/memory.go`、`internal/personal/compact.go`（摘要输入跳过 `context_update`）、`internal/personal/store/types.go`（注释）、`internal/wiring/prompt.go`、`internal/wiring/runtime.go`（注入 `EnvironmentSource`、`SkillsSource`）、hook `SystemContext` 与 lostReply 的生成处(+tests)
- 动作: 段内更新：回合开始比较生效 hash（段记录 hash，被已采纳的最新 `context_update` 覆盖）与当前规则/附加提示 hash，不同则写 `context_update`（user 角色全文，随回合采纳、回放，是带 seq 的 ctxItem，不进 `state.Messages`），替代“以 system prompt 中的版本为准”通知；hook `SystemContext` 与 lostReply 改为 user 角色。按 T3 结论二选一：
  - 成立：`basePrompt` 只留身份与工作约定；环境、规则、记忆索引、skills 列表、附加提示、延迟工具列表进段上下文消息；同一提交删除 `promptStale`/`memoryPromptHook` 与 `contextBuilder.meta` 共享（系统提示词段内不变，不需要重建）。
  - 不成立（降级）：系统提示词 = base + 最新已采纳段记录的快照（规则、记忆索引等仍在系统提示词）；保留回合中途分段后重建本步系统提示词的钩子（由 `memoryPromptHook` 泛化而来）；段上下文消息只承载交接包；记 Operational Discovery。
- 验证: 全量测试；新测试：规则变化后后续回合都看到新规则且只追加一次、回合失败后重判不丢、成立分支下两工作区同模式系统提示词字节相同且 Claude 路径 system 段内不变、降级分支下回合中途分段后下一步系统提示词为新快照；主机最小提示（确认连续多条 user 消息被网关接受）。
- 完成标准: 测试通过。
- 依赖: T5

### T7 分段交接（W2、D4）

- 文件: `internal/personal/compact.go`、`internal/personal/segment.go`、`internal/personal/handoff.go`、`internal/personal/trace.go`(+tests)
- 动作: 阈值与 `:compact` 改为调用分段入口：生成交接包（模板见设计；`previous` = 当前段交接包；输入跳过 `context_update`；附最近 3 条用户请求原文，跳过 Stop hook 注入与“继续”类短句；候选 ref 来自本段外置输出与消息 ref）；写段记录（reason=threshold/manual），段内保留最后一组完整调用/结果；trace 显示分段（原因、前后 token、段号）。
- 验证: 全量测试；新测试：交接包含最近请求原文与 ref、`--from` 后再分段仍保留交接内容、阈值分段后模型请求中段上下文与交接包正确；评测多轮任务在小窗口配置下触发分段并通过；主机最小提示。
- 完成标准: 测试通过；trace 可见。
- 依赖: T6

### T8 会话单飞（W2/W3、D5）

- 文件: `internal/personal/gate.go`、`internal/personal/runner.go`、`internal/personal/compact.go`、`internal/personal/segment.go`(+tests)
- 动作: 按会话的闸门：`Acquire(ctx)`（等待）、`TryAcquire()`、`AcquirePreemptible(ctx) (release, preempted <-chan struct{})`——可抢占持有者在 Run/Compact `Acquire` 时收到取消并在释放后让位；Run、Compact、分段入口共用。段记录为唯一提交点：提交前取消 → compaction 事件 status=cancelled，不写记录与 meta；提交后取消（`compact.go:222` 的 `WithoutCancel` 之后）→ 视为成功；`fail()` 对取消不再记 error。
- 验证: 全量测试（-race）；并发测试：可抢占分段进行中发起 Run/Compact 会取消它并等待、不产生两条段记录；提交前/后取消的两种结果。
- 完成标准: 测试通过。
- 依赖: T7

### T9 定时组件 `AC/schedule`（W3、D9）

- 文件: 新 `pkg/agentcore/schedule/schedule.go`(+test)
- 动作: 实现设计接口并补充 `Handle{Cancel, Reset, Pause, Resume}`：`After`、`Every`、`Idle`、`Touch`、`Close`；`Pause` 停止计时直到 `Resume`（`Resume` 从当下重新计时）；同名替换；同一任务不并发（上次未结束则跳过并记 skipped）；选项：时钟注入（假时钟，不睡眠）、`Observe` 事件回调（scheduled/fired/skipped/cancelled/paused/resumed/done/failed，带名字与耗时）；只依赖标准库。可按用户选定方式派给 suag/omp/codex 各自在独立 worktree 实现同一说明，我复核（门控 + 反向验证 + diff 审读）后择一并入，对比结果记入 gofer plan。
- 验证: 全量测试（-race）；假时钟覆盖：延迟、周期、Idle 被 Touch 推迟、Pause 期间不触发且 Resume 后重新计时、取消与重置、Close 等待运行中任务、同名替换、跳过并发；分层守卫；反向验证（去掉 Touch 重置后 Idle 测试失败）。
- 完成标准: 测试通过。
- 依赖: T0（可与 T4–T8 并行）

### T10 chat 空闲整理（W3、D5、D6）

- 文件: `internal/chat/chat.go`、`internal/chat/render.go`、`internal/config`（`context.idle_after` 支持秒级时长、`context.idle_min_ratio`）、`scripts/ptysmoke/`（新场景）(+tests)
- 动作: chat 创建 Scheduler，注册 `Idle("idle-consolidate", idle_after, fn)`；回合开始 `Pause`，回合结束（Stop 发出后）`Resume`，键盘输入、投递与 `:compact` 调用 `Touch`；fn 检查上下文估计 ≥ `idle_min_ratio` × 阈值后以可抢占方式获取单飞并调用分段入口（reason=idle）；状态行经 `lockedWriter` 输出“空闲整理中…”/“已整理上下文：X → Y token”；事件经 Observe 写入 events；`idle_after: 0` 关闭。
- 验证: 全量测试；假时钟/短阈值测试：回合时长超过 idle_after 时回合中不触发、回合结束后 idle_after 触发；比例不足不触发；输入抢占且不留记录；`make smoke-chat` 新场景（秒级阈值 + fakellm，空闲后出现整理提示、随后输入正常）。
- 完成标准: 测试与冒烟通过。
- 依赖: T8、T9

### T11 文档（W4）

- 文件: `README.md`、`README.zh-CN.md`、`pkg/agentcore/README.md`（schedule）、`config/config.yaml`（`context.*`）、`docs/usage/gofer-personal-agent.md`（用量行、ACP 用量口径）、`CHANGELOG.md` 未发布条目
- 动作: 按代码更新；列出与设计的差异。
- 验证: 文档中的命令在临时 SUAG_HOME 下可执行。
- 完成标准: 文档与实现一致。
- 依赖: T10

### T12 全量与真实验证

- 文件: 无（问题回到对应任务）
- 动作: 容器全量 race、vet、`make smoke-chat`；主机 `go test ./...` 与 staticcheck；主机确认 `suag ps` 为空后备份 `sessions.db`（连同 `-wal`/`-shm`）再 `make install`；主机评测（含多轮）与 T3 基线对比 `first_call_uncached`、prompt 总量、耗时；用 omp 同任务对比。
- 验证: 结果写入 gofer plan；数字对比表。
- 完成标准: 完成定义 1–5 满足。
- 依赖: T11

## 回滚与恢复

- 每个任务独立提交，可按任务 `git revert`；T4–T8 有依赖，回滚时从后往前；T6 是单个提交，回滚后回到“段记录存在、提示词未搬”的可用状态（T4 的状态）。
- 数据：段记录只扩展 compaction 记录字段，`summary` 始终含完整交接包，旧二进制按旧结构读取不丢上下文；`context_update` 开始回放只影响新条目。主机安装前有 `sessions.db` 备份。
- 主机安装回滚：按上一个提交重新 `make install`。
- 中断恢复：按 gofer plan 最后完成的任务与 HEAD 继续（SR1407）。

## 人工 Gate

- 计划批准（本文件）。
- 当前执行请求：批准后另需一句明确的开始执行。
- T9 的派发方式（自建 worktree 派发，或待 gofer 为 suag 建项目后用 `--worktree`）由用户选定。
- T3 结论若为“不成立/无法判定”，T6 按降级方案执行并在汇报中说明（设计 D7 已预先约定）。
- 外部动作 Gate：push 与发版不在本计划授权内。

## 可追溯性

| 设计条目 | 任务 | 验证 |
|---|---|---|
| D1 稳定前缀 | T2、T3、T6 | 三组探针；两工作区系统提示词字节相同；评测 first_call_uncached |
| D2 段记录为准、meta 只作缓存 | T4 | 回合失败不生效、旧会话兼容、旧结构解码测试 |
| D3 段内更新回放 | T6 | 只追加一次、失败重判测试 |
| D4 交接包 | T4、T7 | 最近请求与 ref、`--from` 后再分段测试 |
| D5 会话单飞与提交点 | T8、T10 | 抢占与取消测试 |
| D6 空闲整理条件 | T10 | 回合中不触发、比例不足不触发、冒烟 |
| D7 探针门槛与 Claude 断点 | T2、T3、T5 | 三组探针、请求体测试、真实验证或 UNVERIFIED |
| D8 用量输出 | T1 | 单调递增、ACP 扁平键 = run、gofer fixture、主机实测 |
| D9 定时组件 | T9、T10 | 假时钟测试、反向验证 |
| Q1–Q5 | T6、T7、T10 | 对应测试 |

## 完成 Gate 与剩余工作

完成条件：完成定义 1–5 满足，T12 记录齐全，gofer plan 全部 todo 完成。完成后提出：push 与是否发版；suag-jz0l（循环核心对比）；空闲时提炼记忆候选；people（suag-frgt）。
