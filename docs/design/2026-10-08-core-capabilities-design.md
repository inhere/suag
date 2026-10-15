<!-- template_id: design; template_version: 1.1.1 -->
# Suag 核心能力演进设计

> 状态：Draft 0.2 / 待人工计划批准

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-08 | Claude | 初稿：单库会话条目、压缩与计划续接、tool_search、执行观测、旧控制面清理 |
| 0.2 | 2026-10-08 | Claude | 按合并评审修订：压缩切点与溢出兜底（原 CORE_BLOCKING）；agentcore 存储映射与并发；按缓存友好方式注入计划和工具；观测改为独立尽力写入并在包装层记录；拒绝语义的实现位置和契约变化；交接不写回来源会话；Gofer `--from` 记为 deferred |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

## 规划声明

- 思考模式：`RIGOROUS`（SR1408）。
- 核心目标：长会话不溢出上下文，压缩后和新会话都能按计划继续；工具多时模型只看到需要的工具；每次模型/工具调用都有可查询的记录。
- scope freeze：本文范围限于下文 A–E 五项。新增模块、外部协议或数据迁移，只有直接服务核心验收时才纳入，否则记为 deferred（`expansion_policy=DEFER_OR_REQUEST`）。
- 治理档位：未投产、个人使用、可回退到旧二进制和旧库，属于低暴露度（SR1409）。评审封顶为一轮合并轴评审（已完成，结论见修订记录 0.2），保留人工计划 Gate。
- 停止条件：评审的 `CORE_BLOCKING` 问题已在 0.2 中修订；待确认事项答复后即进入实施计划。

## 背景与目标

用户在 `inhere-tools/local/suag-idea.md` 中要求：按最新思路设计、不背历史包袱；自带计划/进度存储，新开会话也能续接；自带 `tool_search`；执行观测要从一开始设计，并随每项功能一起实现。同批遗留的问题有：`suag-7d1f`（两份数据库）、`suag-2gsc`（个人命令仍依赖 HITL）、`suag-8cu3` 中的 S3（拒绝一次后短路）和 S4（plan 只从最终文本解析）。

用户可见结果：

1. 长对话在接近上下文上限前自动压缩。压缩后模型仍然知道目标、已完成和未完成的事项，以及当前计划。单个很长的回合（一条输入加几十次工具调用）也不会让会话卡死。
2. `suag run|chat --from <session>` 开一个新会话，继承上一个会话的摘要、计划和已加载的工具，继续工作。
3. 接入很多 MCP 工具时，请求里只带内置工具和已加载的工具，其余工具由模型通过 `tool_search` 按需加载。
4. `suag stats` 和 `suag trace <session>` 能看到每次模型调用和工具调用的耗时、token、缓存、错误和拒绝；`suag export` 能导出 jsonl 用于回放和排障。

## 名词

| 名词 | 含义 |
|---|---|
| entry | 会话中按序追加的一条记录：用户消息、助手消息、工具结果、压缩、计划、交接等 |
| compaction | 把较早的 entry 总结为一条摘要 entry。原 entry 保留不删，构建上下文时从摘要之后开始取 |
| plan | 模型用 `update_plan` 声明的步骤表，整表替换，最多一个 `in_progress` |
| handoff | 新会话的首条 entry，携带来源会话的摘要、计划和已加载的工具 |
| deferred tool | 不随请求下发 schema 的工具，只在系统提示里列出名称和一句话说明，被 `tool_search` 命中后才加载 |
| event | 一次模型调用、工具调用、拒绝、压缩、搜索或回合的观测记录 |

## 范围与非目标

范围（只涉及 suag 仓库）：

- A 存储：单库、会话条目化，替换 Host session 的表和按会话拆分的 `runs.db`。
- B 压缩、计划与交接。
- C 工具分档与 `tool_search`。
- D 执行观测：事件表，以及 `stats` / `trace` / `export` 命令。
- E 清理：移除 Host session 服务、HITL、未使用的 tracing、记忆/recap 旧表、无生产者的挂起回合恢复逻辑；审批移到工具包装层，实现新的拒绝语义。

非目标 / deferred：会话分叉和树形分支（SR1405，目前没有使用证据）；OTel/OTLP 导出；按模型价格换算成本；子 Agent 与多 Agent；向量检索式的工具搜索；Gofer 侧为 `--from` 增加模板变量（需要时另行向 Gofer 提出）。

契约：stream-json 只新增字段和事件。ACP 有一处语义变化，见流程"拒绝语义"。Gofer 的 hook、transcript、`session_family` 互通都沿用现有字段。

## 已确认事实与规范

- 现有会话库为 `~/.suag/db/suag.db`，表来自 `db/migrations/20260411-100000-create-host-session-foundation.sql`（sessions/turns/runs/run_events/external_tasks/recaps）和 `20260416-120000-add-host-memory-candidates.sql`（memory_candidates）。每个会话另有一个 agentcore 库 `~/.suag/db/agentcore/host/<session>/runs.db`（`internal/personal/runner.go:237`）。所以"两份 db"实际是 1 个主库加 N 个会话库。
- 续接时，`completedHistory` 把该会话所有已完成 run 的全部消息拼接进上下文（`internal/personal/runner.go:465`），没有压缩，也没有截断；失败或取消的 run 不进上下文（`runner.go:481`）。
- agentcore 每次工具调用都追加一对消息：assistant{ToolCall} 和 tool{ToolCall, ToolResult}（`pkg/agentcore/runtime.go:498`），所以每一对调用和结果在存储里都是完整的。每一步都调用 `ListTools`（`runtime.go:133`）。
- agentcore 的 `Storage` 保存整份 `RunState`（`pkg/agentcore/storage/sqlite/sqlite.go:176`），`LoadRunAuthority` 会把 LoadRun 和 LoadMessages 合起来使用。
- 生产代码中没有任何地方调用 `ctrl.Yield`。ACP 审批是在 BeforeTool 里同步调用 RequestPermission（`internal/acpserver/policy.go`），所以 `waitingRun`（`runner.go:444`）这条路径没有生产者。
- 现在 ACP 拒绝审批时，BeforeTool 返回 ErrPermissionDenied，整轮结束并返回 `refusal`（`internal/acpserver/server.go:226`）。CLI 的 MCP 拒绝在 `cliMCPGate.ExecuteTool` 里返回工具错误（`internal/app/cli_mcp.go`）。
- provider 出错时不会调用 AfterModel（`runtime.go:362`）；BeforeTool 拒绝时会跳过 AfterTool（`runtime.go:444`）。
- ACP 的 `plan` 更新从最终答复文本中解析（`internal/acpserver/server.go:257`）。
- `internal/platform/tracing` 只被 `legacy/` 引用。`internal/host/session/service.go` 导入 `internal/ops/hitl`。
- Claude provider 曾经把 tool 消息渲染成重复的 tool_use，tool_result 从未发送。此缺陷在本设计之外由修复批 B11 处理；本设计中"tool_result 紧跟 tool_use"的约束依赖这一修复。
- 参考实现调研（2026-10-08，pi、oh-my-pi、DeepSeek Harness、Codex、Claude Code），见"决策"表的出处列。调研只作为方向参考。
- 适用规范：SR1107、SR1403/SR1405、SR1409、GR104、GR105。

## 总体方案

**一个库、一条条目流、一个观测表。** 会话内容统一以 entry 追加到同一个 SQLite 库，agentcore 通过存储适配器读写，不再为每个会话单独开库。上下文由"压缩摘要（作为第一条 user 消息）+ 摘要之后的 entry"构成。模型调用由 provider 包装层记录，工具调用（包括拒绝）由工具注册表包装层记录，都写入 events 表，与 entry 同库，但是独立的尽力写入。命令行查询和导出都基于这些表。

观测随功能一起实现：A 阶段先落 events 表和两个记录包装层。之后的每个阶段，新功能都要定义自己的 event 类型，并把"能在 `suag trace` 中看到"写进验收。

## 架构

```text
 CLI (run/chat)        ACP stdio            Gofer hook / transcript（不变）
        \                 |                     /
         +------ internal/personal.Runner ------+
                 |  context builder：摘要(user) + 近期 entry
                 |  compactor：阈值触发，切点对齐，溢出兜底
                 |  tool registry 包装：审批/拒绝、deferred 加载、tool event
                 v
            pkg/agentcore runtime（每步 ListTools → 模型 → 工具）
                 |  provider 包装：model event（TTFT/错误/重试/用量）
                 v
        store（单库 ~/.suag/db/sessions.db，WAL + busy_timeout）
           sessions | entries | turns | events
                 ^
    suag stats / trace / export（只读查询）
```

数据模型（新库 `~/.suag/db/sessions.db`，用 GR105 命名的迁移文件维护）：

| 表 | 关键列 | 说明 |
|---|---|---|
| sessions | id, title, cwd, created_at, updated_at, meta_json | meta 存放追加提示、规则哈希、生效模型、已加载的 deferred 工具名 |
| entries | session_id, seq, turn_id, type, json, created_at；PK(session_id, seq) | type 取值：user / assistant / tool / compaction / plan / handoff / context_update |
| turns | id, session_id, status, stop_reason, state_json, started_at, ended_at | 约定 run_id = turn_id；`state_json` 存 agentcore RunState |
| events | id, session_id, turn_id, kind, name, status, started_at, duration_ms, ttft_ms, input/output/cache_read/cache_write tokens, error, attrs_json | kind 取值：model / tool / denial / turn / compaction / tool_search |

存储适配器（不改 `pkg/agentcore` 的接口）：

- SaveRun/LoadRun 读写 `turns.state_json`。
- AppendMessages 在一个事务里追加一批 entry，seq 在会话内递增。
- LoadMessages 只返回 user/assistant/tool 类型的 entry。
- stream-json 的 `result.run_id` 和 `input_required.run_id` 取 turn_id，字段名保持不变。
- 多进程同时访问（chat、run、acp 并存）时，开启 WAL、busy_timeout 5s；会话写入仍由现有会话锁串行化。
- 删除无生产者的挂起回合恢复逻辑（SR1405），turns.status 只保留 running/completed/failed/cancelled。

会话累计用量直接用 `SUM(events)` 得出，`suag-6hqq` 的 metadata 方案届时废弃。

## 关键流程

**回合与压缩（B）**

1. 回合开始时，取会话最新的 compaction entry，以及它之后属于已完成回合的 entry。失败或取消回合的 entry 不进上下文，与现状一致。
2. 估算上下文 token：字符数/4 近似，并用上一次模型调用报告的 input_tokens 校正。超过 `window − reserve`（默认 reserve 16k）时触发压缩：（2026-10-15 起改为按字符类型加权——ASCII 约 4 字节/token、CJK 一个字一个 token，见 CHANGELOG。）
   - 切点从尾部往前数约 20k token 的位置开始，**向后**对齐到最近的可切位置，也就是宁可少留。可切位置按优先顺序为：user entry 之前；或者一个完整的调用/结果对之后。切点不会落在调用和结果之间。
   - 摘要用当前模型按固定模板生成：目标、约束、进展（完成/进行中/阻塞）、当前计划、关键决策、涉及文件、下一步。
   - 写一条 compaction entry，内容为 `{summary, first_kept_seq, tokens_before, tokens_after}`，并记一条 compaction event。
   - 构建上下文时，摘要固定作为第一条 user 消息注入，满足 Anthropic"第一条必须是 user"的要求，也不改动 system，以免整段缓存失效。
3. 压缩后再估算一次。如果仍然超限（例如单个工具结果极大），对进入上下文的工具结果做截断（保留头尾，单条从 16k 字符起逐级减半，最低 2k），并记一条 compaction 错误 event。截断后仍超限，就结束回合并报错，不会无限重试。原文始终完整保存在 entry 中。
4. 一轮执行过程中，在每次调用模型前做同样的估算，必要时执行第 2 步，所以单个超长回合也能压缩。
5. chat 支持 `:compact` 手动压缩。

**计划（B）**

- 内置 `update_plan(explanation?, plan:[{step,status}])` 工具，整表替换，校验最多一个 `in_progress`，写一条 plan entry。
- 平时不额外注入计划：`update_plan` 的调用本身就在上下文里。压缩时，把最新计划写进摘要，所以压缩后也不丢失。这样只在压缩时改写一次前缀，对 prompt cache 友好。
- ACP 收到 plan entry 时发 `plan` session/update，不再解析最终文本（关闭 S4）。stream-json 新增 `plan` 事件。

**交接与续接（B）**

- `suag run|chat --from <session>`：新建会话，首条是 handoff entry，内容为 `{source, summary, plan, loaded_tools}`。
  - 来源会话有最新摘要且之后没有新内容时，直接复用该摘要。
  - 否则只读来源会话的 entry，在内存中生成一份交接摘要，写进新会话。不往来源会话写任何东西，也不需要取来源会话的锁。
- 同一会话的续接（`--resume`、ACP `session/load`）照常走"摘要 + 近期 entry"。

**工具分档（C）**

- direct：内置工具、`tool_search`、`update_plan`。
- deferred：MCP 工具。`tools.deferred` 可选 `auto | always | never`，默认 `auto`：MCP schema 总量超过上下文窗口 10% 时延迟加载，否则直接下发。
- 系统提示按 server 分组，列出 deferred 工具的"名称 — 一句话说明"，说明截断到 160 字符。列表按名称排序，会话内保持稳定。
- `tool_search(query, limit≤10)`：对名称和说明做分词匹配并打分；`select:a,b` 表示精确选择。命中的工具按名称排序，追加到 direct 工具之后；已加载集合写入 sessions.meta，下一次模型调用起下发。每次加载都会让工具前缀的缓存失效，tool_search event 会记录这一点，便于在 trace 中核对。
- 模型直接调用一个未加载的 deferred 工具时，返回工具错误"该工具尚未加载"，附上它的 schema，并同时把它加入已加载集合，下一步即可正常调用。
- 放行策略不变：已加载只表示模型能看到这个工具，执行时仍走 allowlist 和审批。

**观测（D）**

- model event：在 provider 包装层记录，覆盖成功、错误和重试，包含 TTFT（流式时）和用量。
- tool event / denial event：在工具注册表包装层记录，覆盖执行、错误和拒绝。
- 写入与 entry 同库，但不同事务。写失败只告警，不影响回合。
- `suag stats [--since 7d] [--session id]`：按模型和工具汇总调用数、错误率、拒绝数、p50/p95 耗时、token、缓存命中率。
- `suag trace <session> [--turn id]`：按时间线列出回合、模型调用、工具调用、拒绝、压缩、搜索事件。
- `suag export <session>`：把 entries 和 events 合并导出为 jsonl，可附 `--redact` 去掉工具参数。

**拒绝语义（E，S3）**

- 实现位置：CLI 和 ACP 的审批都移到工具注册表包装层，参照现有的 `cliMCPGate.ExecuteTool`。被拒绝的调用返回工具错误给模型，回合继续。BeforeTool 不再用 ShortCircuit 结束回合。
- 同一回合累计被拒 3 次后，结束回合。
- 契约：
  - stream-json：本回合只要发生过拒绝，回合结束时都发 `input_required`，并带被拒工具列表。与现有约定（首次拒绝即出 `input_required`）的差别在于：模型绕开拒绝把活干完后，Gofer 仍然知道当时缺了授权。
  - ACP（语义变更）：拒绝后回合可以正常完成，stopReason 由 `refusal` 改为 `end_turn`；仅在累计 3 次拒绝时返回 `refusal`。Gofer 的 ACP 客户端两者都能处理，但这项变化要写进 `docs/usage/gofer-personal-agent.md`。

## 安全、数据、运维与回滚

- 数据：新库使用新文件名，旧的 `suag.db` 和 `agentcore/` 目录不读、不改、不删。回退到 v0.2.x 二进制后仍能使用旧会话。
- 旧会话不迁移（见 Q1）。影响：切换后，旧的 session ID 用 `--resume`、ACP `session/load` 都会报"未找到"；Gofer 已登记的旧会话也无法送话唤醒或接管。
- 导出：`export` 默认包含工具参数和结果，可能含敏感内容；只写到本地文件或 stdout，`--redact` 可以去掉参数。
- events 写失败不影响回合。数据库体积由 `suag gc --older-than` 控制（deferred，先记录）。
- 清理 E：删除 `internal/host/session` 服务、`internal/ops/hitl`、`internal/platform/tracing`，以及 recaps/memory_candidates/external_tasks 相关代码。`legacy/` 的去留见 Q2。

## 决策

| 编号 | 决策 | 出处/理由 |
|---|---|---|
| D1 | 使用 SQLite 单库和线性 entry，不做树形分叉；适配器保持 agentcore 接口不变 | Codex 用 SQLite 状态库；没有分叉需求（SR1405） |
| D2 | 压缩条目化：原文保留；切点向后对齐，可落在完整调用/结果对之后；摘要作为首条 user 消息；溢出时逐级截断兜底 | pi 的 CompactionEntry 与 Goal/Progress/Next 模板；评审 CORE_BLOCKING |
| D3 | `update_plan` 整表替换并持久化；只在压缩时写进摘要，不逐轮注入 | Codex update_plan 只发事件不回读；DSH/oh-my-pi 会持久化；对缓存友好 |
| D4 | 工具分 direct/deferred 两档，加阈值 auto 模式；直接调用未加载工具时报错并自动加载 | Claude Code ToolSearch 与 `ENABLE_TOOL_SEARCH=auto`；Codex tool_search |
| D5 | 观测先记原始事件，在包装层尽力写入，用 SQL 汇总，暂不做 OTel | Codex rollout-trace 的 observe first；oh-my-pi stats |
| D6 | 审批移到工具包装层；拒绝后回合继续，累计 3 次结束；有拒绝的回合结束时发 `input_required` | `suag-8cu3` S3；Gofer 需要知道缺了哪些授权 |
| D7 | 交接摘要不写回来源会话 | 不改变来源会话，也不需要来源会话的锁 |

## 待确认事项

- Q1 旧会话：不迁移（推荐，旧库原样保留可回退，影响见上一节），还是提供一次性导入命令？
- Q2 `legacy/` 目录：直接删除（推荐，`v0.1.0` tag 已保存那时的代码，而且它目前本就编译不过），还是继续保留？
- Q3 压缩模型：默认使用当前模型（推荐），另提供可选配置 `llm.compact_model`，还是必须单独配置一个小模型？

## 结论与人工计划 Gate

建议分四个实施阶段，前提是修复批 B11（provider 工具消息映射）已完成：

1. A：新库、存储适配器、两个记录包装层，以及 `trace` 的最小版本。
2. B：压缩、计划、交接。
3. C：tool_search。
4. D/E：stats/export、审批迁移与拒绝语义、旧代码清理。

每个阶段的验收都包括功能测试和 `suag trace` 中可见的事件证据。

本设计在人工批准前不进入计划编写，也不实施。需要的批准语句："批准 suag 核心能力演进设计 0.2 进入计划（Q1/Q2/Q3 按推荐）"，或逐项给出答复。
