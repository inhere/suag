<!-- template_id: plan; template_version: 1.2.0 -->
# Suag 核心能力演进 实施计划

> 状态：Draft 0.2 / 待人工计划批准

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-08 | Claude | 初稿：按设计 0.2 拆为 A–E 五阶段、15 个任务 |
| 0.2 | 2026-10-08 | Claude | 按合并评审修订：用量切换移到 T05（原 CORE_BLOCKING）；T04 拆为 T04a/T04b；T06→T07 改为串行；固定三层工具包装的顺序；拒绝上限改由 BeforeModel 结束回合；provider 包装保留流式能力，并在 Pool 内逐个 provider 记录；补齐 T03/T06/T12 规格和测试隔离兜底；store 改列为新模块候选 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

## 规划声明

- 思考模式：`RIGOROUS`（SR1408）。
- 核心目标：按已批准的设计 0.2，交付单库条目化存储、压缩/计划/交接、tool_search、执行观测和旧代码清理；每个阶段都有可运行的验证和 `suag trace` 事件证据。
- scope freeze：只实现设计 0.2 的 A–E 五项，以及 Q1/Q2/Q3 的推荐答复（旧会话不迁移、删除 `legacy/`、压缩默认用当前模型并支持可选的 `llm.compact_model`）。设计里的 deferred 项不实施；实施中发现的新需求一律 `DEFER_OR_REQUEST`。
- review budget：低暴露度（SR1409），一轮合并轴评审（已完成，1 条 CORE_BLOCKING 和其余建议已在 0.2 中吸收）。之后不再开新的评审轮次。
- 停止条件：计划批准后，执行还需要一句单独的当前执行请求。任一任务触发 `Semantic Amendment` 或 `Ownership Conflict` 时停止，回到计划 Gate。

## 目标与完成定义

目标：交付设计 0.2 列出的四项用户可见结果：自动压缩不溢出、`--from` 续接、`tool_search` 按需加载、`stats`/`trace`/`export` 可观测；同时完成 E 项清理。

完成定义（全部满足）：

1. 新会话写入 `~/.suag/db/sessions.db`，不再创建 `suag.db` 和每会话的 `runs.db`。CLI run/chat 和 ACP 新建、续接、`session/load` 都能工作，旧库文件保持不动。stream-json 的字段和 `status` 取值（`COMPLETED`、`INPUT_REQUIRED` 等）与现在一致。
2. 用 mock provider 构造一个超过窗口阈值的会话（含单个超长回合），能自动压缩并继续回答；`suag trace` 中能看到 compaction 事件。
3. `update_plan` 写出的计划能通过 ACP `plan` 更新和 stream-json `plan` 事件看到；压缩后的摘要里包含当前计划；`--from` 开的新会话能继承摘要、计划和已加载工具。
4. MCP schema 超过阈值时，请求里只有 direct 工具和已加载工具；`tool_search` 命中后，下一次模型调用起下发对应工具；未加载就直接调用会得到"尚未加载"的错误，同时自动加载。
5. `suag stats`、`suag trace`、`suag export` 能输出模型调用（包括 Pool 内逐个 provider 的尝试）、工具调用、拒绝、压缩、搜索事件；provider 出错和工具拒绝都会计入；会话累计用量由 model 事件汇总得出。
6. 审批在工具包装层完成：拒绝后回合继续，累计 3 次结束；有拒绝的回合结束时发 `input_required`；ACP 的 stopReason 按设计变化，并写入文档；ACP 的 tool_call 状态通知保持不变。
7. 删除 `internal/host/session`、`internal/ops/hitl`、`internal/data`、`db/migrations`、`miglite.yaml`、`internal/platform/memory`、`internal/platform/tracing`、`legacy/`，以及 `internal/config/paths.go` 中的旧路径函数；`go list -deps ./cmd/suag` 中不再出现 hitl。
8. 容器内 `go vet`（linux/windows/darwin）、`gofmt`、`go test -race ./...` 通过；Windows 主机 `go test ./...` 通过。
9. 关闭 `suag-7d1f`、`suag-2gsc`；README、`docs/usage/gofer-personal-agent.md`、AGENTS.md 的入口说明已同步。

## 范围、排除项与授权

范围：仅 `inhere-tools/suag` 仓库（Git 根即该目录）。

排除项：设计 0.2 的 deferred 项（分叉/树形会话、OTel、成本换算、子 Agent、向量检索、Gofer 侧 `--from` 模板变量、`suag gc`）；旧会话导入；`pkg/agentcore` 公共接口的破坏性修改（只允许新增）。

授权：
- 本计划获批，并且收到单独的当前执行请求后，可以按任务本地修改并按功能点本地提交（工作区默认授权）。
- 不 push、不发版、不改动 Gofer 仓库。
- 不读、不改、不删用户现有的 `~/.suag/db/suag.db` 和 `~/.suag/db/agentcore/`。测试一律使用临时 `SUAG_HOME`（见 T01 的测试隔离兜底）。
- 删除 `legacy/` 等目录属于仓库内的源码删除，可由 git 恢复，在本计划授权范围内。
- `database_url`/`DATABASE_URL` 不再影响会话库的位置（它们对应的是旧表结构），新库位置只由 `SUAG_HOME` 决定，并写入文档（T13）。

- `host_or_non_offline_action=REQUIRED`

说明：唯一需要在主机上执行的动作，是经 `gofer job run -p my-tools-dev --runner local -a exec` 在 Windows 主机上运行只读的 `go test`。项目 key `my-tools-dev` 已通过 `gofer project list` 确认，修复批的回归也用过它。这属于工作区 CLAUDE.md 已约定的常规主机验证，不涉及发布、部署或外部消息。

## 输入与批准证据

- 设计：`docs/design/2026-10-08-core-capabilities-design.md` Draft 0.2（提交 69c6dd6）。
- 用户批准原话（2026-10-08）："批准 suag 核心能力演进设计 0.2 进入计划（Q1/Q2/Q3 按推荐）"。
- 设计评审：一轮合并轴评审，1 条 CORE_BLOCKING 和 5 条 SHOULD 已在 0.2 中吸收。
- 计划评审：一轮合并轴评审（针对 0.1，e0383f7），1 条 CORE_BLOCKING、10 条 SHOULD、4 条 NIT，已在本版吸收。
- 前置修复：修复批 `plan-20261008-222122-6cf0bc1e` 已完成，其中 B11（c2960b5）和 B12（b45e38f）是本计划的前提。

## Capability Discovery

### Capability decisions

| capability_id | required_capability | searched_candidates | direct_reuse | thin_adapter_or_owner_extension | decision | proven_gap | duplication_and_lifecycle_risk |
|---|---|---|---|---|---|---|---|
| CAP-01 | 单库会话存储（sessions/entries/turns/events） | internal/host/session（SQLite 仓库与服务）；pkg/agentcore/storage/sqlite；internal/data（迁移执行） | 否：host/session 的表是 turn/run/run_events 模型，没有 entry 和 event，而且依赖 hitl | 否：在 host/session 上扩展，既要保留旧表，又带着 hitl 依赖，与"不背历史包袱"冲突 | MINIMAL_NEW_MODULE | 现有表结构与设计 0.2 的数据模型不兼容，迁移还依赖源码路径（internal/app/data.go 用 runtime.Caller 定位） | 旧仓库在 T12 删除，不会两套并存 |
| CAP-02 | agentcore Storage 映射到单库 | pkg/agentcore/storage/sqlite；storage/files | 否：两者都是每个 run 一套表或目录 | 在 store 上实现 api.Storage 适配器（run_id=turn_id） | THIN_ADAPTER | 现有实现无法与 entries 共用 seq | 无，pkg 原实现保留给库用户 |
| CAP-03 | 模型/工具观测 | internal/platform/tracing（未接入主链）；agentcore hooks；cli_mcp 的 usage 收集 | 否：tracing 只有 span 内存模型，hook 拿不到错误 | provider 包装层（Pool 内逐个 provider）和 Registry 包装层，写入 events 表 | OWNER_EXTENSION | 评审已证实 AfterModel/AfterTool 在失败路径上不会被调用；Pool 的 fallback 在 agentcore 外部看不到 | tracing 包在 T12 删除 |
| CAP-04 | 压缩与上下文构建 | pkg/agentcore/context（TranscriptWindow、MemoryFirst） | 部分：窗口截断不等于摘要压缩 | 在 personal 的 historyContext 上扩展 | OWNER_EXTENSION | 已有构建器不生成摘要，也不写回 entry | 无 |
| CAP-05 | 计划工具 | acpserver planEntries（从文本解析） | 否 | 在 internal/platform/tools/native 中新增 `update_plan`，通过回调接口写出，native 包不依赖 personal | OWNER_EXTENSION | 文本解析无法持久化，也不能稳定续接 | planEntries 在 T07 删除 |
| CAP-06 | 工具分档与搜索 | runtimecaps、tools.Registry、cliMCPGate | 部分：Registry 的 ListTools 本来就每步调用 | 包装 Registry：过滤 ListTools，并注册 `tool_search` | OWNER_EXTENSION | 现有 Registry 没有可见性概念 | 无 |
| CAP-07 | stats/trace/export 命令 | cmd/suag 现有子命令框架（capp） | 是 | 新增子命令，直接用 SQL 查询 store | DIRECT_REUSE | none | 无 |
| CAP-08 | 工具层审批与拒绝 | cliMCPGate.ExecuteTool；acpserver permissionHook | 部分：CLI 已在工具层；ACP 在 BeforeTool 中返回 ShortCircuit | 统一的审批 Registry 包装，加上 BeforeModel hook 处理拒绝上限 | OWNER_EXTENSION | 评审证实：BeforeTool 拒绝会结束整轮；ExecuteTool 返回的错误会被转成工具结果，无法结束回合 | permissionHook 只保留 tool_call 状态通知 |

### New module candidates

| candidate_id | capability_id | proposed_module | searched_candidates | direct_reuse_gap | thin_adapter_or_owner_extension_gap | proven_gap | unique_owner_and_lifecycle | deletion_or_merge_handling |
|---|---|---|---|---|---|---|---|---|
| NEW-01 | CAP-01 | internal/personal/store | internal/host/session；pkg/agentcore/storage/sqlite；internal/data | host/session 的表模型不同，并依赖 hitl；agentcore sqlite 是每个 run 一套表 | 扩展 host/session 要保留旧表和 hitl，与设计 Q1（不迁移、不背包袱）冲突 | 表结构不兼容，且迁移依赖源码路径 | 唯一使用者是 internal/personal；随 personal 的生命周期，迁移嵌入二进制 | T12 删除 host/session 和 internal/data，由 store 取代，不并存 |

### Rejected new tools

| rejected_candidate | capability_id | deletion_test_and_reason |
|---|---|---|
| 独立的 internal/store 顶层包 | CAP-01 | 只有 personal 使用，放进 internal/personal/store 子包即可；删掉这个候选不影响验收 |
| OTel 导出器 | CAP-03 | 设计已 deferred；删掉不影响任何完成定义 |

## 前置检查与 fail-closed 条件

- workspace baseline（2026-10-08 规划时）：Git 根 `inhere-tools/suag`，分支 `main`，HEAD `e0383f7`（计划 0.1 提交后），`git status --short` 为空；没有 untracked 文件需要保护。实施前再执行一次 `git status --short`；出现非本计划产生的 dirty 文件时视为 `Ownership Conflict`，停止。
- 依赖：Go 1.25；modernc.org/sqlite 已在 go.mod 中；`embed` 是标准库。不新增第三方依赖；确需新增时按 `Semantic Amendment` 处理。
- 环境：容器内 Go；Windows 主机经 gofer exec job 运行。
- 测试隔离兜底（在 T01 落实）：`internal/app`、`internal/acpserver`、`internal/personal`、`cmd/suag` 各有一个 `TestMain`，统一把 `SUAG_HOME` 设为临时目录，并设置 `SUAG_GOFER_HOOKS=off`；单个测试仍可以用 `t.Setenv` 覆盖。
- 预期路径与符号（规划证据，不是封闭白名单）：
  - 新增：`internal/personal/store/`、`internal/personal/{compact.go,context.go,observe.go,handoff.go}`、`internal/platform/tools/visibility.go`、`internal/platform/tools/native/{plan.go,tool_search.go}`。
  - 修改：`internal/personal/{runner.go,usage.go}`、`internal/platform/llm/pool.go`（观测挂钩）、`internal/acpserver/{server.go,policy.go,usage.go}`、`internal/app/{cli.go,chat.go,acp.go,cli_mcp.go,capabilities.go}`、`internal/config/paths.go`、`cmd/suag/main.go`。
- fail-closed：
  - 任一任务的 focused 测试失败，不进入依赖它的任务。
  - 发现需要破坏 `pkg/agentcore` 公共接口时，停止。
  - 测试一旦触及真实的 `~/.suag`，立即停止并修复测试隔离。

## 波次与依赖

| 波次 | 任务 | 依赖 | 并行 |
|---|---|---|---|
| W1 存储与观测底座 | T01 → T02 → T03 → T04a → T04b → T05 | 顺序 | 否 |
| W2 会话能力 | T06 → T07 → T08 | T05 | 否（都改 runner.go） |
| W3 工具与查询 | T09、T10 并行 | T08 | 是（评审核对过，文件不重叠） |
| W4 审批与清理 | T11 → T12 → T13 | T09、T10 | 否 |
| W5 回归与收尾 | T14 → T15 | T13 | 否 |

工具包装顺序（T05、T09、T11 共同遵守）：从外到内依次是 observe → visibility → approval → base registry。
- 未加载的工具在 visibility 层就返回"尚未加载"，不会触发审批。
- approval 层拒绝时返回类型化的哨兵错误 `tools.ErrDenied`，observe 层据此记一条 denial event，与普通工具错误区分。
- 每一层都保留 `Get(name)`，以及判断工具来源的能力（Kind、MCP server 名），供审批层和 cliMCPGate 使用。

执行方式：每个任务由会话内的 sonnet 子 agent 实施，本会话复核 diff 并运行验证。进度用 gofer plan todo 跟踪，每个任务一个 todo，按功能点本地提交。

## 任务

### T01 store：嵌入式迁移、单库打开、测试隔离兜底

- 文件:
  - 新增 `internal/personal/store/{store.go,migrations/20261008-230000-create-core-store.sql,store_test.go}`。
  - `internal/config/paths.go`：新增 `ResolveCoreStorePath`，返回 `$SUAG_HOME/db/sessions.db`，不读 database_url。
  - 在 `internal/app`、`internal/acpserver`、`internal/personal`、`cmd/suag` 中新增或修改 `main_test.go` 的 `TestMain`。
- 动作:
  - 用 `embed.FS` 嵌入迁移，SQL 采用 GR105 格式（`-- Migrate:UP`），建 sessions/entries/turns/events 四张表及必要索引（entries PK(session_id,seq)；events 索引 session_id、started_at）。
  - `Open(path)` 在 DSN 中用 `_pragma` 设置 `journal_mode(WAL)`、`busy_timeout(5000)`、`foreign_keys(1)`，保证连接池中每个连接都生效；用迁移记录表幂等执行迁移。
  - TestMain 统一设置临时 `SUAG_HOME` 和 `SUAG_GOFER_HOOKS=off`。
- 验证: `go test -race ./internal/personal/store/`：新建库、重复打开幂等、两个连接并发写不出现 `database is locked`、`PRAGMA journal_mode` 返回 wal。之后跑一次 `go test ./internal/app/... ./internal/acpserver/... ./cmd/...`，确认 TestMain 没有破坏现有测试。
- 完成标准: 测试通过；在临时 `SUAG_HOME` 下 `Open` 后只生成 `sessions.db`（以及 WAL 附属文件）。
- 依赖: 无。

### T02 store：会话、条目、回合、事件 API

- 文件: `internal/personal/store/{sessions.go,entries.go,turns.go,events.go}` 及测试。
- 动作:
  - 会话：CreateSession/GetSession/UpdateSession（meta_json），`ErrSessionNotFound`。
  - 条目：AppendEntries（同一事务内分配 seq）、ListEntries（按 since_seq、turn_id、types 过滤）、`LatestEntry(session, type)`。
  - 回合：CreateTurn/UpdateTurn/GetTurn、UpsertTurnState。
  - 事件：AppendEvent/ListEvents、`SumUsage(session)`（只汇总 `kind=model`，避免与 turn event 重复计数）。
  - entry type 和 event kind 用常量表示。entry type 包含 context_update：规则更新、追加提示变化这类上下文更新会写入记录，供 trace 审计，但不会重放进后续上下文，与现状的一次性注入一致。
- 验证: `go test -race ./internal/personal/store/`：seq 连续；类型和 turn 过滤正确；LatestEntry 正确；SumUsage 只计 model 事件。
- 完成标准: 测试通过；每个文件不超过 1000 行。
- 依赖: T01。

### T03 agentcore Storage 适配器

- 文件: `internal/personal/store/agentstore.go`、`agentstore_test.go`。
- 动作: 实现 `api.Storage`，run_id 等于 turn_id。
  - SaveRun 按 upsert 写入 `turns.state_json`（Runner 会先 CreateTurn 再 SaveRun）。
  - LoadRun 在 state_json 为空或回合不存在时，按 agentcore 约定返回未命中（参照 runtime.go 的 LoadRun 处理）。
  - AppendMessages 把消息转成 user/assistant/tool 类型的 entry，保留 ToolCall、ToolResult、is_error 和 B12 保留的助手文本。
  - LoadMessages 按 turn_id 和这三种类型过滤，按 seq 排序。
- 验证: 用 `pkg/agentcore` 的 mock provider 跑一次带两次工具调用的 run，再 LoadMessages，断言消息与 runtime 内存中的状态一致；另断言 LoadRun 未命中时的行为；同一会话两个回合的消息互不串扰。
- 完成标准: 测试通过。
- 依赖: T02。

### T04a personal 包切换到 store

- 文件: `internal/personal/{runner.go,usage.go}`，以及 `runner_test.go`、`usage_test.go`、`append_prompt_test.go`。
- 动作:
  - Runner 的 `Config.Repository` 换成 store，改用 T03 的适配器；删除 `completedHistory`、`waitingRun` 和每会话 `runs.db`。
  - 历史改为读已完成回合的 user/assistant/tool entry。
  - 会话 metadata（规则哈希、追加提示）迁入 sessions.meta_json。
  - 会话累计用量仍沿用现有的 usageCollector 和 meta，切换到 SumUsage 放在 T05。
  - personal 包对外导出会话类型与 `ErrSessionNotFound`，替代 `session.Session` 和 `session.ErrSessionNotFound`。
- 验证: `go test -race ./internal/personal/...`（测试改用 store）。
- 完成标准: personal 包不再导入 `internal/host/session`。
- 依赖: T03。

### T04b 装配切换与契约回归

- 文件: `internal/app/{cli.go,chat.go,acp.go}`；删除 `internal/app/data.go`；`internal/acpserver/{server.go,usage.go}`；测试 `chat_hooks_test.go`、`cli_test.go`、`usage_output_test.go`、`acpserver/server_test.go`；新增 golden 测试。
- 动作:
  - `prepareHostSessionDatabase` 和 `session.NewSQLiteRepository` 改为 `store.Open(ResolveCoreStorePath())`。
  - 替换 cli.go 中对 `session.ErrSessionNotFound` 和 repo 的引用（cli.go:47、211、396 附近），以及 chat.go:51。
  - stream-json 字段及 `status` 取值保持不变（run_id 取 turn_id）。
- 验证:
  - `go test -race ./internal/app/... ./internal/acpserver/... ./cmd/...`。
  - 新增 e2e：CLI run 两回合后 `--resume` 续接；ACP 先 `session/new` 再 `session/load` 续接，用量继续累计。
  - 新增 golden 测试：stream-json 的 session、result、input_required 三类行的字段集合和 status 取值，与切换前的样本一致（切换前先生成样本）。
- 完成标准: 测试通过；在临时 `SUAG_HOME` 下运行后，`db/` 中只有 `sessions.db`；`go list -deps ./cmd/suag` 中已不含 `internal/host/session`。
- 依赖: T04a。

### T05 观测包装层、用量切换与最小版 `suag trace`

- 文件: 新增 `internal/personal/observe.go`；`internal/platform/llm/pool.go`（逐个 provider 的观测挂钩）；`internal/platform/tools/registry.go`（`ErrDenied` 哨兵错误，以及包装层需要的查询接口）；`internal/personal/{runner.go,usage.go}`；`internal/acpserver/usage.go`；`cmd/suag/main.go`（`trace` 子命令）；以及测试。
- 动作:
  - provider 包装层同时实现 `Generate` 和 `Stream`（内层不支持流式时退回 Generate），保证 runtime 的流式类型断言依然成立。
  - Pool 提供一个观测回调，每尝试一个 provider（含 fallback）记一条 model event：provider、model、耗时、TTFT（流式时）、用量、错误、attempt 序号。
  - 压缩摘要的那次模型调用也走这个包装层。
  - Registry 的 observe 层（最外层）记 tool event；遇到 `ErrDenied` 时记 denial event；cliMCPGate 的拒绝改为返回 `ErrDenied`。
  - Runner 记 turn event。事件写入独立于 entry 事务，失败只 `slog.Warn`。
  - 会话累计用量改由 `store.SumUsage` 得出，删除 usageCollector 中的 meta 累计。
  - `suag trace <session> [--turn id] [--json]` 按时间线输出。
- 验证:
  - Pool 配两个 mock provider，第一个报错、第二个成功，断言有两条 model event（attempt 1 失败、attempt 2 成功）。
  - 一次工具错误、一次 MCP 拒绝，断言各有对应 event。
  - 流式 mock 经包装后仍走 Stream 路径，并记录 TTFT。
  - 注入 event 写库失败，回合仍成功。
  - ACP `session/load` 后 usage_update 的累计值等于 SumUsage。
  - `trace` 输出包含以上事件。
- 完成标准: 测试通过；完成定义第 5 条中与模型、工具、拒绝和用量相关的部分得到验证。
- 依赖: T04b。

### T06 压缩与上下文构建

- 文件: 新增 `internal/personal/{compact.go,context.go}` 及测试；`internal/personal/runner.go`（接入）；`internal/app/chat.go`（`:compact`）；`internal/config/llm.go`（可选 `llm.compact_model`，未配置时用当前模型）。
- 动作:
  - 估算：ContextBuilder 在每个 step 中先于 listTools 和模型调用执行（runtime.go 的 step 循环）。估算值 = 上一次模型调用的 input_tokens（第一次调用时用 runner 注入的 system 提示与工具 schema 字符数/4）+ 之后新增消息的字符数/4。
  - 阈值为 `window − reserve(16k)`；近期部分保留约 20k token。
  - 切点向后对齐，只落在 user entry 之前，或完整的调用/结果对之后。切点按 entries 的 seq 计算；当前回合内的切点映射到 `state.Messages` 的下标。builder 只返回视图，不修改 state。
  - 摘要按固定模板生成（包含当前计划，由 `store.LatestEntry(session,"plan")` 读取；T07 之前不存在则为空）；写 compaction entry `{summary, first_kept_seq, tokens_before, tokens_after}`，并记 compaction event。
  - 只采用已完成回合写下的 compaction：压缩所在的回合失败或取消时，该 compaction 不会被后续上下文使用，与"失败回合不进上下文"一致。
  - 摘要作为首条 user 消息注入。
  - 仍超限：工具结果从 16k 字符起逐级减半截断，最低 2k，并记错误事件；还超限就结束回合并报错。
- 验证（测试中显式设置 MaxSteps≥50）:
  - 多回合累计超过阈值；单回合 1 条输入加 40 次大结果工具调用；单条工具结果超大。三种情况都能压缩或截断后继续，或按设计报错。
  - 断言切点不落在调用和结果之间，且首条消息是 user。
  - 压缩回合被取消后，下一回合不使用该回合的 compaction。
  - 配置 `llm.compact_model` 与不配置两种情况。
- 完成标准: 测试通过；完成定义第 2 条得到验证。
- 依赖: T05。

### T07 `update_plan` 与计划事件

- 文件: 新增 `internal/platform/tools/native/plan.go` 及测试；`internal/acpserver/server.go`（删除 planEntries 文本解析，改为由 plan entry 驱动）；`internal/app/cli.go`（stream-json `plan` 事件）；`internal/personal/runner.go`（实现 PlanSink，写 plan entry 并发事件）。
- 动作:
  - native 包定义 `PlanSink` 接口，在构造工具时注入，native 包不依赖 personal。
  - `update_plan(explanation?, plan:[{step,status}])` 整表替换，校验 status 取值和"最多一个 in_progress"，写 plan entry。
  - ACP 发 `plan` session/update；stream-json 输出 `{"type":"plan",...}`。
- 验证: 工具单测；ACP 测试断言 plan 更新来自工具调用，而不是最终文本；stream-json 测试断言 plan 事件；T06 的压缩摘要测试中出现最新计划。
- 完成标准: 测试通过；S4（plan 只从最终文本解析）关闭。
- 依赖: T06。

### T08 `--from` 交接

- 文件: 新增 `internal/personal/handoff.go` 及测试；`internal/app/cli.go`（run/chat 的 `--from` 选项）。
- 动作:
  - 来源会话的最新已完成 compaction 之后没有新内容时，直接复用该摘要；否则只读来源会话的 entry，在内存中生成摘要（复用 T06 的摘要函数，经观测包装层），不写回来源会话，也不取来源会话的锁。
  - 新会话首条写 handoff entry `{source, summary, plan, loaded_tools}`，loaded_tools 写入新会话的 meta。
  - `--from` 与 `--resume`、`--session-id`（已存在的会话）互斥。
- 验证: 测试断言新会话的首条消息包含摘要和计划；来源会话的 entry 数量不变；loaded_tools 被继承；互斥参数报错。
- 完成标准: 测试通过；完成定义第 3 条得到验证。
- 依赖: T07。

### T09 工具分档与 `tool_search`

- 文件: 新增 `internal/platform/tools/visibility.go`、`internal/platform/tools/native/tool_search.go` 及测试；`internal/app/capabilities.go`（装配，读取配置 `tools.deferred`）；`internal/personal/runner.go`（系统提示列出 deferred 工具；已加载集合与 meta 同步）。
- 动作:
  - visibility 层：ListTools 只返回 direct 工具和已加载工具；已加载的按名称排序，放在 direct 之后。
  - auto 模式：deferred 工具 schema 总长度超过窗口 10% 时延迟加载。
  - 系统提示按 server 分组，按名称排序，列出"名称 — 说明"，说明截断到 160 字符。
  - `tool_search(query, limit≤10)` 做分词打分；`select:a,b` 精确选择。
  - 执行未加载的 deferred 工具时，返回错误并附 schema，同时自动加载。
  - 写 tool_search event，attrs 中记录 `cache_invalidated=true`（已加载集合发生变化时）。
- 验证: 用 30 个假 MCP 工具测试三种模式；搜索命中后下一步 ListTools 中出现该工具；直接调用未加载工具得到错误并完成加载，且不触发审批；续接会话后已加载集合仍在；系统提示的分组和截断正确。
- 完成标准: 测试通过；完成定义第 4 条得到验证。
- 依赖: T08。

### T10 `suag stats` 与 `suag export`

- 文件: 新增 `cmd/suag/observe_cmd.go`、`internal/personal/store/report.go` 及测试。
- 动作:
  - `stats [--since 7d] [--session id] [--json]`：按模型和工具汇总调用数、错误率、拒绝数、p50/p95 耗时、token、缓存命中率；p50/p95 在 Go 中计算。
  - `export <session> [--redact] [-o file]`：把 entries 和 events 按时间合并输出 jsonl；`--redact` 去掉工具参数。
- 验证: 用固定的事件数据断言汇总数值；export 每行都是合法 JSON，`--redact` 后不含参数字段。
- 完成标准: 测试通过；完成定义第 5 条全部得到验证。
- 依赖: T08（与 T09 并行，不改 runner.go 和 capabilities.go）。

### T11 工具层审批与拒绝语义

- 文件: `internal/app/{cli_mcp.go,acp.go}`（acp.go 的工厂签名改为能把 ACP 连接交给审批层）；`internal/acpserver/{policy.go,server.go}`；新增审批包装（放在 `internal/platform/tools/approval.go`，或由 app 组装）；`internal/personal/observe.go`（拒绝计数）及测试；`docs/usage/gofer-personal-agent.md`。
- 动作:
  - approval 层统一 CLI allowlist 和 ACP RequestPermission，拒绝时返回 `ErrDenied`。
  - `context.Canceled`（审批被取消）向上传播并中止回合，不转成工具结果。
  - ACP 只读模式下调用被禁工具（`ErrReadOnly`）按一次拒绝处理（返回工具错误，回合继续）。
  - 拒绝上限：approval 层按回合计数；一个 BeforeModel hook 在计数达到 3 时用 ShortCircuit 结束回合，并带"拒绝上限"标记。ACP 把该标记映射为 `refusal`，stream-json 映射为 `input_required`。
  - 回合内有过拒绝但正常结束时，stream-json 发 `input_required`，带 `denied_tools`；ACP 返回 `end_turn`。
  - permissionHook 只保留 tool_call 的 pending/in_progress/completed 通知，审批逻辑迁出。
  - 删除 cliMCPGate.AfterModel 中"重复请求被拒工具即结束"的旧短路逻辑。
  - 在文档中写明 ACP 的语义变化。
- 验证: CLI 和 ACP 各覆盖以下用例：被拒 1 次后完成（ACP 为 end_turn，stream-json 发 input_required 并带 denied_tools）；被拒 3 次后结束（ACP 为 refusal）；审批被取消时中止；read-only 下不执行写工具且按拒绝计数；ACP 的 tool_call 状态通知序列与改动前一致。
- 完成标准: 测试通过；完成定义第 6 条得到验证。
- 依赖: T09、T10。

### T12 清理旧代码

- 文件:
  - 删除：`internal/host/session/`、`internal/ops/hitl/`、`internal/data/`、`db/migrations/`、`miglite.yaml`、`internal/platform/memory/`、`internal/platform/tracing/`、`legacy/`。
  - 删除 `internal/config/paths.go` 中的 `ResolveSessionDBPath`、`ResolvePersonalSessionDBPath`、`ResolveHostAgentcoreRunRoot`、`ResolvePersonalAgentcoreRunRoot`、`ResolveHostMemoryDraftRoot`。
  - 清理 `go.mod`/`go.sum`（`go mod tidy`）和 `.gitignore` 中的相关条目。
- 动作: 先用 `go list -deps ./cmd/suag` 和 grep 确认没有引用，再删除；`pkg/agentcore` 全部保留。
- 验证: `go build ./... && go vet ./...`；`go list -deps ./cmd/suag | grep -E 'hitl|host/session|internal/data|tracing'` 输出为空。
- 完成标准: 完成定义第 7 条得到验证。
- 依赖: T11。

### T13 文档同步

- 文件: `README.md`、`docs/usage/gofer-personal-agent.md`、`AGENTS.md`（"当前项目入口"一节）；旧设计文档 `docs/design/2026-10-06-personal-agent-redesign-design.md` 顶部加一行"存储部分已被 2026-10-08 设计取代"。
- 动作: 说明以下内容：
  - 新库路径，以及 `database_url` 不再生效。
  - 旧库不迁移及其影响：旧 ID 无法续接；Gofer 中登记的旧会话无法唤醒或接管。
  - 新命令和选项：`--from`、`stats`/`trace`/`export`、`tools.deferred`、`llm.compact_model`、`:compact`。
  - 拒绝语义的变化。
- 验证: 人工核对；`grep -n "suag.db\|runs.db\|database_url" README.md docs/usage/*.md` 只剩"旧库保留"和"不再生效"相关的说明。
- 完成标准: 文档与实现一致。
- 依赖: T12。

### T14 全量回归

- 文件: 无新增（只修复回归中发现的问题，归类为 Corrective）。
- 动作:
  - 容器内运行 `go vet ./...`、`GOOS=windows go vet ./...`、`GOOS=darwin go vet ./...`、`gofmt -l cmd internal pkg`、`go test -race -count=1 ./...`。
  - Windows 主机运行 `gofer job run -p my-tools-dev -a exec --runner local --cwd inhere-tools/suag --sync -- go test -count=1 ./...`。
- 验证: 以上命令的输出（记录 job id）。
- 完成标准: 全部通过，对应完成定义第 8 条。
- 依赖: T13。

### T15 端到端冒烟与收尾

- 文件: 无代码改动（发现问题时归类为 Corrective，并回到 T14）。
- 动作:
  - 在临时 `SUAG_HOME` 下用 mock provider（`docs/usage/llm-mock-usage.md`）构建二进制并依次运行：`suag run` 两回合 → `suag run --from` → `suag trace` → `suag stats` → `suag export`，记录输出摘要。
  - 关闭 `suag-7d1f`、`suag-2gsc`；在 gofer plan 中留下验收评论；把契约变化（ACP stopReason、`database_url`、新命令）知会 gofer 会话。
- 验证: 命令输出中可见压缩、计划、交接、搜索事件。
- 完成标准: 完成定义第 1–9 条全部满足。
- 依赖: T14。

## 回滚与恢复

- 每个任务独立提交，可以 `git revert` 单个提交回退；T12 的删除可整体 revert 恢复。
- 用户数据：新库使用新文件名，旧库不动。回退到当前 HEAD 构建的二进制，即可继续使用旧会话。
- 恢复点：每个波次结束时，在 gofer plan 的 todo 备注中记录 HEAD、已通过的测试和下一个任务（不超过 15 行，SR1407）。

## 人工 Gate

1. 计划批准（本文）。
2. 当前执行请求：可以与计划批准同句给出，例如"批准计划 0.2 并开始实施"。
3. 不涉及 Secret、push、发布、部署或外部消息，不需要外部动作 Gate。实施中如果出现这类需求，属于 `Semantic Amendment`，停止并请求。

## 可追溯性

| 需求/决策（设计 0.2） | 任务 | 验证 |
|---|---|---|
| A 单库存储、D1 线性 entry、适配器、WAL/busy_timeout | T01–T03、T04a、T04b | store、agentstore 单测；CLI/ACP 续接 e2e；stream-json golden |
| 删除挂起回合恢复逻辑 | T04a | personal 测试 |
| D2 压缩切点、首条 user、溢出兜底（评审 CORE_BLOCKING） | T06 | 三种溢出场景及取消回合的测试 |
| D3 计划持久化、只在压缩时写进摘要 | T07、T06 | ACP 和 stream-json 的 plan 测试；摘要包含计划 |
| D7 交接不写回来源会话 | T08 | 来源会话 entry 数量不变 |
| D4 tool_search、auto 模式、未加载报错并加载、排序追加 | T09 | 三种模式和续接的测试 |
| D5 观测在包装层、尽力写入、用量汇总 | T05、T10 | Pool 尝试、错误、拒绝事件测试；SumUsage；stats/export 测试 |
| D6 拒绝语义 | T11 | CLI/ACP 拒绝用例 |
| E 清理、Q2 删除 legacy | T12 | deps 中不再出现 hitl |
| Q1 旧会话不迁移 | T04b、T13 | `db/` 中只有 sessions.db；文档说明影响 |
| Q3 压缩模型 | T06 | 配置与未配置两种情况的测试 |
| 观测随功能实现 | T05–T11 | 每个功能都有对应 event 断言 |

## 完成 Gate 与剩余工作

- 完成条件：完成定义第 1–9 条全部满足，T14 和 T15 的证据已记录到 gofer plan 中。
- 剩余工作：设计 0.2 的 deferred 项和 `suag-iw3u` 中的小项不在本计划内，完成后在报告中列出。
- 中途中断时，按 6 行中断卡汇报：状态、已完成、当前/已验证、未完成、首个失败边界、下一步。
