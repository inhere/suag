<!-- template_id: design; template_version: 1.1.1 -->
# Suag 核心能力演进设计

> 状态：Draft 0.1 / 待人工计划批准

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-08 | Claude | 初稿：单库会话条目、压缩与计划续接、tool_search、执行观测、旧控制面清理 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

## 规划声明

- 思考模式：`RIGOROUS`（SR1408）。
- 核心目标：长会话不溢出上下文，压缩后和新会话都能按计划继续；工具多时模型只看到需要的工具；每次模型/工具调用都有可查询的记录。
- scope freeze：本文范围限于下文 A–E 五项。新增模块、外部协议或数据迁移，只有直接服务核心验收时才纳入，否则记为 deferred（`expansion_policy=DEFER_OR_REQUEST`）。
- 治理档位：未投产、个人使用、可回退到旧二进制和旧库，属于低暴露度（SR1409）。评审封顶为一轮合并轴评审，保留人工计划 Gate。
- 停止条件：评审没有 `CORE_BLOCKING` 问题，且待确认事项都已有答复，即进入实施计划。

## 背景与目标

用户在 `inhere-tools/local/suag-idea.md` 中要求：按最新思路设计、不背历史包袱；自带计划/进度存储，新开会话也能续接；自带 `tool_search`；执行观测要从一开始设计，并随每项功能一起实现。同批遗留的问题有：`suag-7d1f`（两份数据库）、`suag-2gsc`（个人命令仍依赖 HITL）、`suag-8cu3` 中的 S3（拒绝一次后短路）和 S4（plan 只从最终文本解析）。

用户可见结果：

1. 长对话在接近上下文上限前自动压缩。压缩后模型仍然知道目标、已完成和未完成的事项，以及当前计划。
2. `suag run|chat --from <session>` 开一个新会话，继承上一个会话的摘要和计划继续工作；Gofer 也可以在新 job 里这样做。
3. 接入很多 MCP 工具时，请求里只带内置工具和已加载的工具，其余工具由模型通过 `tool_search` 按需加载。
4. `suag stats` 和 `suag trace <session>` 能看到每次模型调用和工具调用的耗时、token、缓存、错误；`suag export` 能导出 jsonl 用于回放和排障。

## 名词

| 名词 | 含义 |
|---|---|
| entry | 会话中按序追加的一条记录：用户消息、助手消息、工具结果、压缩、计划、交接等 |
| compaction | 把较早的 entry 总结为一条摘要 entry。原 entry 保留不删，构建上下文时从摘要之后开始取 |
| plan | 模型用 `update_plan` 声明的步骤表，整表替换，最多一个 `in_progress` |
| handoff | 新会话的首条 entry，携带来源会话的摘要和计划 |
| deferred tool | 不随请求下发 schema 的工具，只在系统提示里列出名称和一句话说明，被 `tool_search` 命中后才加载 |
| event | 一次模型调用、工具调用或回合的观测记录 |

## 范围与非目标

范围（只涉及 suag 仓库）：

- A 存储：单库、会话条目化，替换 Host session 的表和按会话拆分的 `runs.db`。
- B 压缩、计划与交接。
- C 工具分档与 `tool_search`。
- D 执行观测：事件表，以及 `stats` / `trace` / `export` 命令。
- E 清理：移除 Host session 服务、HITL、未使用的 tracing、记忆/recap 旧表；确定 S3 拒绝语义。

非目标 / deferred：会话分叉和树形分支（SR1405，目前没有使用证据）；OTel/OTLP 导出；按模型价格换算成本；子 Agent 与多 Agent；单轮执行中途压缩（先用工具输出截断兜底，见 B）；向量检索式的工具搜索。

ACP 和 stream-json 的现有字段保持兼容，只做新增。Gofer 侧不需要改动。

## 已确认事实与规范

- 现有会话库为 `~/.suag/db/suag.db`，表来自 `db/migrations/20260411-100000-create-host-session-foundation.sql`（sessions/turns/runs/run_events/external_tasks/recaps）和 `20260416-120000-add-host-memory-candidates.sql`。每个会话另有一个 agentcore 库 `~/.suag/db/agentcore/host/<session>/runs.db`（`internal/personal/runner.go:237`）。所以"两份 db"实际是 1 个主库加 N 个会话库。
- 续接时，`completedHistory` 把该会话所有已完成 run 的全部消息拼接进上下文（`internal/personal/runner.go:465`），没有压缩，也没有截断。会话越长，请求越大，最终超出上下文。
- `pkg/agentcore` 每一步都调用 `ToolRegistry.ListTools` 再请求模型（`pkg/agentcore/runtime.go:133`），所以工具集合可以在一轮执行中途变化。
- ACP 的 `plan` 更新目前从最终答复文本中解析（S4 已实现），旧 plan 不会清除（`suag-8cu3`）。
- `internal/platform/tracing` 没有被 cmd/internal 的主链引用。用量只累计在内存里，`suag-6hqq` 正把会话累计写入 metadata，作为过渡。
- `internal/host/session/service.go` 直接导入 `internal/ops/hitl`（`suag-2gsc`）。
- 参考实现调研（2026-10-08，pi、oh-my-pi、DeepSeek Harness、Codex、Claude Code），摘要见本文"决策"一节的出处列。调研只作为方向参考，具体结论都在本仓库代码中复核。
- 适用规范：SR1107（DPI 流程）、SR1403/SR1405（最小充分、无证据不抽象）、SR1409（暴露度）、GR104（单文件≤1000 行）、GR105（迁移命名）。

## 总体方案

**一个库、一条条目流、一个观测表。** 会话内容统一以 entry 追加到同一个 SQLite 库。agentcore 通过一个存储适配器读写这些 entry，不再为每个会话单独开库。上下文由"最近的压缩摘要 + 当前计划 + 摘要之后的 entry"构成。模型调用和工具调用由 agentcore hook 记录到 events 表。命令行查询和导出都基于这两张表，不另建存储。

观测随功能一起实现：A 阶段先落 events 表和记录 hook。之后的 B、C 阶段，每项功能都要定义自己的 event 类型，并把"能在 `suag trace` 中看到"写进验收。

## 架构

```text
 CLI (run/chat)        ACP stdio            Gofer hook / transcript（不变）
        \                 |                     /
         +------ internal/personal.Runner ------+
                 |  context builder：摘要 + 计划 + 近期 entry
                 |  compactor：阈值触发，生成摘要 entry
                 |  tools：direct 集合 + 已加载的 deferred 集合 + tool_search/update_plan
                 v
            pkg/agentcore runtime（每步 ListTools → 模型 → 工具）
                 |  hooks：event recorder（模型/工具/回合）
                 v
        store（单库 ~/.suag/db/sessions.db）
           sessions | entries | turns | events
                 ^
    suag stats / trace / export（只读查询）
```

数据模型（新库 `~/.suag/db/sessions.db`，用 GR105 命名的迁移文件维护）：

| 表 | 关键列 | 说明 |
|---|---|---|
| sessions | id, title, cwd, created_at, updated_at, meta_json | meta 存放追加提示、规则哈希、生效模型、已加载的 deferred 工具名 |
| entries | session_id, seq, turn_id, type, json, created_at；PK(session_id, seq) | type 取值：user / assistant / tool_result / compaction / plan / handoff / context_update |
| turns | id, session_id, status, stop_reason, started_at, ended_at | 回合状态，支持等待中的回合恢复与锁语义 |
| events | id, session_id, turn_id, kind, name, status, started_at, duration_ms, ttft_ms, input/output/cache_read/cache_write tokens, error, attrs_json | kind 取值：model / tool / turn / compaction / tool_search |

会话累计用量直接用 `SUM(events)` 得出，`suag-6hqq` 的 metadata 方案届时废弃。

## 关键流程

**回合与压缩（B）**

1. 回合开始：读取会话最新的 compaction entry、最新的 plan entry，以及 compaction 之后的 entry。
2. 估算上下文 token（字符数/4 的近似，加上上一次模型调用报告的 input_tokens 校正）。超过 `window − reserve`（默认 reserve 16k）时，先做压缩：
   - 从尾部往前保留约 20k token 的 entry 作为近期部分；切点只落在 user entry 边界，不切开工具调用和结果。
   - 用当前模型按固定模板生成摘要：目标、约束、进展（完成/进行中/阻塞）、关键决策、涉及文件、下一步。
   - 写一条 compaction entry，内容为 `{summary, first_kept_seq, tokens_before}`，并记一条 compaction event。
3. 进入上下文的工具结果单条截断到上限（默认 16k 字符，保留头尾，原文仍完整存在 entry 中），这样单轮中途也不容易溢出。
4. chat 支持 `:compact` 手动压缩。

**计划（B）**

- 内置 `update_plan(explanation?, plan:[{step,status}])` 工具，整表替换，校验最多一个 `in_progress`，写一条 plan entry。
- ACP 收到 plan entry 时发 `plan` session/update，不再解析最终文本（关闭 S4）。stream-json 新增 `plan` 事件。
- 最新计划放在摘要之后注入上下文，压缩后不丢失。

**交接与续接（B）**

- `suag run|chat --from <session>`：新建会话，首条是 handoff entry，内容为 `{source, summary, plan}`。来源会话如果没有可用摘要，先对其执行一次压缩生成摘要，来源会话本身不变。
- 同一会话的续接（`--resume`、ACP `session/load`）照常走"摘要 + 计划 + 近期 entry"。

**工具分档（C）**

- direct：内置工具、`tool_search`、`update_plan`。
- deferred：MCP 工具。`tools.deferred` 可选 `auto | always | never`，默认 `auto`：MCP schema 总量超过上下文窗口 10% 时延迟加载，否则直接下发。
- 系统提示按 server 分组，列出 deferred 工具的"名称 — 一句话说明"，说明截断到 160 字符。
- `tool_search(query, limit≤10)`：对名称和说明做分词匹配并打分；`select:a,b` 表示精确选择。命中的工具加入会话的已加载集合（写入 sessions.meta），下一次模型调用起随 ListTools 下发。每次搜索记一条 tool_search event。
- 放行策略不变：已加载只表示模型能看到这个工具，执行时仍走 allowlist 和 ACP 审批。

**观测（D）**

- event recorder 实现为 agentcore hook，在 Before/After Model 和 Before/After Tool 时机记录。TTFT 由流式 provider 包装测得。写入与 entry 同库同事务，写失败只告警，不中断回合。
- `suag stats [--since 7d] [--session id]`：按模型和工具汇总调用数、错误率、p50/p95 耗时、token、缓存命中率。
- `suag trace <session> [--turn id]`：按时间线列出回合、模型调用、工具调用、压缩、搜索事件。
- `suag export <session>`：把 entries 和 events 合并导出为 jsonl，可附 `--redact` 去掉工具参数。

**拒绝语义（E，S3）**

工具被拒绝后，给模型返回工具错误，回合继续。同一回合累计被拒 3 次后，结束回合：stream-json 发 `input_required`，ACP 返回 `end_turn`。不再在第一次拒绝后把后续所有工具调用都短路。

## 安全、数据、运维与回滚

- 数据：新库使用新文件名，旧的 `suag.db` 和 `agentcore/` 目录不读、不改、不删。回退到 v0.2.x 二进制后仍能使用旧会话。
- 旧会话不迁移（见待确认 Q1）。切换后，旧的 session ID 用 `--resume` 或 ACP `session/load` 会报"未找到"。
- 导出：`export` 默认包含工具参数和结果，可能含敏感内容；只写到本地文件或 stdout，`--redact` 可以去掉参数。
- events 写失败不影响回合。数据库体积由 `suag gc --older-than` 控制（deferred，先记录）。
- 清理 E：删除 `internal/host/session` 服务、`internal/ops/hitl`、`internal/platform/tracing`，以及 recaps/memory_candidates/external_tasks 相关代码。`legacy/` 的去留见 Q2。

## 决策

| 编号 | 决策 | 出处/理由 |
|---|---|---|
| D1 | 使用 SQLite 单库和线性 entry，不做树形分叉 | Codex 用 SQLite 状态库；pi 有树形会话但我们没有分叉需求（SR1405） |
| D2 | 压缩条目化：原文保留，切点不落在工具结果上，摘要用固定模板 | pi 的 CompactionEntry 与 Goal/Progress/Next 模板 |
| D3 | `update_plan` 整表替换并持久化，压缩后重新注入 | Codex update_plan 只发事件不回读；DSH 和 oh-my-pi 都会持久化 |
| D4 | 工具分 direct/deferred 两档，加阈值 auto 模式 | Claude Code ToolSearch、`ENABLE_TOOL_SEARCH=auto`；Codex tool_search |
| D5 | 观测先记原始事件，用 SQL 汇总，暂不做 OTel | Codex rollout-trace 的 observe first；oh-my-pi stats |
| D6 | 拒绝后回合继续，累计 3 次才结束 | 避免一次拒绝就让整轮失效（`suag-8cu3` S3） |

## 待确认事项

- Q1 旧会话：不迁移（推荐，旧库原样保留可回退），还是提供一次性导入命令？
- Q2 `legacy/` 目录：直接删除（推荐，`v0.1.0` tag 已保存那时的代码，而且它目前本就编译不过），还是继续保留？
- Q3 压缩模型：默认使用当前模型（推荐），另提供可选配置 `llm.compact_model`，还是必须单独配置一个小模型？

## 结论与人工计划 Gate

建议分四个实施阶段：A（新库、存储适配器、event recorder，以及 `trace` 的最小版本）→ B（压缩、计划、交接）→ C（tool_search）→ D/E（stats/export 与旧代码清理）。每个阶段的验收都包括功能测试和 `suag trace` 中可见的事件证据。

本设计在人工批准前不进入计划编写，也不实施。需要的批准语句："批准 suag 核心能力演进设计 0.1 进入计划（Q1/Q2/Q3 按推荐）"，或逐项给出答复。
