<!-- template_id: plan; template_version: 1.2.0 -->
# 中文 session_search S1（混合匹配、cursor 分页与过滤）实施计划

> 状态：Draft 0.2 / 已批准实施（2026-10-17 用户：“批准四份计划，开始实施，授权主机测试和真实模型验证”）

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.2 | 2026-10-17 | suga（gofer job） | 按预算 / 中文检索计划评审 r1 关闭 S-M1–S-M2；补齐检索工具结果排除、快照 snippet/锁/LRU、接口顺序与基线范围。 |
| 0.1 | 2026-10-17 | suga（gofer job） | 依据已批准设计 Draft 0.3 起草 S1 实施计划；明确零迁移、按词分流、进程内快照分页、过滤与 cwd 下推的任务边界。 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

## 目标与完成定义

目标：按 `docs/design/2026-10-16-session-search-cjk-design.md` Approved Draft 0.3 实施 **S1**，让中文 1/2/3+ 字符及中英混合查询保持正确召回，同时提供稳定的进程内 cursor 分页、kind/时间过滤和 SQL cwd 下推；不改索引、不做 S2 bigram。

完成定义（全部满足）：

1. `>=3` rune 的词进入现有 trigram `MATCH`，1–2 rune 的词只作为同一条 SQL 的 `LIKE` 过滤条件；短词绝不进入 MATCH，混合查询不会静默放宽 AND 约束；纯短词继续按既有时间序。
2. `EntrySearch`/等效结果结构支持 `Kinds`、`Since`/`Until`、workspace/current 条件与 cursor；cursor 为 `v1.<snapshot_id>.<offset>`，使用进程内最多 500 条、10 分钟 TTL、最多 64 个快照的加锁 LRU 稳定快照；首查快照保存 `(session_id, seq, role, snippet, created_at)`，翻页按偏移切片，不重新查询；单页上限与总快照上限分离，失效时从头查询并说明。
3. `kind`、半开时间窗、`current`/`workspace`/`all` scope 和 `IncludeAgents` 组合正确；workspace 条件在 SQL 下推，当前会话始终包含，空 cwd 排除，路径按分隔符边界匹配；Windows 使用设计批准的 `lower()` ASCII 折叠并固定已知非 ASCII 差异。
4. `session_search` 新增可选 `cursor`、逗号分隔多值 `kind`、`since`、`until`，结果只有在有下一页时追加 `next_cursor`；`recall`、`ref=out:` 行扫描、redact 和既有调用保持兼容；按设计不扩展 CLI 的 cursor/kind 分页语义。
5. S1 不新增表、列、索引、迁移或持久化快照；旧调用的零值字段和旧输出断言继续通过，游标不进入 SQL，不可信/不匹配游标安全地从头开始。
6. 现有单测、等价性测试、工具端到端测试、固定种子 benchmark 和真实 `sessions.db` 副本的基线测量完成；检索本身没有交互式 chat 改动，因此不新增 ptysmoke 场景。能使用真实 provider 时完成一次中文检索并翻页的核对，否则记录 `UNVERIFIED_REAL_PROVIDER`。

## 范围、排除项与授权

- 范围：`internal/personal/store/fts.go`、相关 store 测试与新 benchmark、`internal/personal/ledger.go` 及其测试、`pkg/agentcore/tools/native/ledger.go` 及测试、必要的 sessions CLI 兼容回归、测试数据和文档说明。
- 允许的实现：同一条只读参数化 SQL 中组合 MATCH 与 LIKE；增加进程内快照缓存、cursor 编解码、kind/时间/cwd 条件；修改 native ledger 工具的可选参数和结果渲染。
- 排除：S2 bigram/第二张索引、unicode61 tokenizer 替换、迁移/回填实现、长期记忆、向量/语义检索、跨 `SUGA_HOME`、trace/stats 分页与 kind 过滤、redact 规则修改、自动检索质量评测体系、CLI cursor/kind 分页产品面。
- `recall` 的 offset/limit 和 `ref=out:` 语义不改；`CWDPrefix` 保留原有裸前缀/大小写敏感语义，新 workspace 结构与它并存。
- 本计划只授权本地实施与按功能点本地提交；不 push、不 tag、不发版，不改 `.gofer/tracker`。
- `host_or_non_offline_action=REQUIRED`（T7/T8 含主机全量测试、staticcheck、真实 `sessions.db` 副本基线与真实 provider；计划批准不自动授权这些出机器动作，需在人工 Gate 另行确认。）

## 输入与批准证据

- 主设计：`docs/design/2026-10-16-session-search-cjk-design.md` Draft 0.3 / 已批准（2026-10-16 用户：“全部按建议批准”；Q1–Q8 均按文中建议选项定稿）。
- 调研与实测：设计来源 `docs/research/2026-10-16-feature-strengthening-analysis.md` §3 #4/#5、设计 §B 的 SQLite 3.53.4 + modernc 实测；短词 MATCH 会返回 0 或静默丢弃约束，unicode61 连续 CJK 不可用。
- 现有索引事实：`entries_fts` 已为 trigram，独立 rowid 通过 `(session_id, seq)` 回连；S1 不改变 schema 或迁移。
- 相关现有能力：`ftsMatchExpr`、`escapeLike`、`likeSnippet`、`pathWithin`、`observeScopeFrom`、`redact.Clean`、`recall` 的 offset 分页与 ledger 工具测试。

## Capability Discovery

### Capability decisions

| capability_id | required_capability | searched_candidates | direct_reuse | thin_adapter_or_owner_extension | decision | proven_gap | duplication_and_lifecycle_risk |
|---|---|---|---|---|---|---|---|
| CAP-01 | trigram/LIKE 按词分流 | `internal/personal/store/fts.go:73-140,189-203` 的 `EntrySearch`、`SearchEntries`、`ftsMatchExpr`、`escapeLike`、`likeSnippet` | 部分：既有 MATCH/LIKE 分支与排序可复用 | 在 `internal/personal/store/fts.go:103-162` 原位扩展 where/order 构造 | OWNER_EXTENSION | 现状按“任一短词”整体退化 LIKE，混合查询无法保持长词索引与短词过滤 | 保留一处 term 分流，禁止另建搜索实现 |
| CAP-02 | 结果过滤与 SQL scope | `internal/personal/store/fts.go:142-162` 的 sessions join、`internal/personal/ledger.go:91-179` 的 `pathWithin`/workspace 逻辑、`internal/personal/store/sessions.go:99-110` 的 `SessionCwds` | 部分：既有 scope 语义与边界参照可复用 | store 参数扩展 + `internal/personal/ledger.go:101-124` Ledger scope 适配 | OWNER_EXTENSION | 现状 workspace 先读全部 cwd 再拼 session ID，不能 SQL 下推 | SQL 是检索 owner；Go 参照逻辑只留测试 |
| CAP-03 | 稳定分页 | `pkg/agentcore/tools/native/ledger.go:26-38` 的查询边界、`internal/personal/store/fts.go:64-71` 的检索上限 | 部分：现有 limit 约定可复用，但无稳定快照 cursor | `internal/personal/store/fts.go` 原位或同包新文件增加 snapshot/cache/cursor | MINIMAL_NEW_MODULE | 现有能力没有 `session_search` cursor 与稳定快照，重跑 SQL 会因新增 entry/排序漂移 | 快照 owner 只在 store 内部，TTL/容量固定且不能落库；不得与 recall 分叉 |
| CAP-04 | native 工具参数与渲染 | `pkg/agentcore/tools/native/ledger.go:26-38,55-122,152-` 的 `LedgerQuery`、`LedgerSearcher`、`renderLedgerHits`、`redact.Clean` | 是：工具协议、searcher 接口和 redact 路径直接复用 | 在 `pkg/agentcore/tools/native/ledger.go:26-122` 原位扩展可选参数与 `SearchResult` 传递 | OWNER_EXTENSION | 当前 schema 无 kind/time/cursor，结果无 `next_cursor` | `recall` 维持独立协议；工具层只做校验/渲染 |
| CAP-05 | 测试/性能基线 | `internal/personal/store/fts_test.go:1-`、`internal/personal/ledger_test.go:1-`、`pkg/agentcore/tools/native/ledger_test.go:1-` | 是：现有 store/Ledger/native 测试夹具与 temp store 可复用 | 在上述测试 owner 中增加等价性测试、benchmark 与 DB 副本测量 | OWNER_EXTENSION | 仓库当前没有本期 benchmark 与 SQL 下推等价守卫 | 固定种子和参照实现只用于测试，不进入生产 |

### New module candidates

| candidate_id | capability_id | proposed_module | searched_candidates | direct_reuse_gap | thin_adapter_or_owner_extension_gap | proven_gap | unique_owner_and_lifecycle | deletion_or_merge_handling |
|---|---|---|---|---|---|---|---|---|
| NEW-01 | CAP-03 | `internal/personal/store/fts_cursor.go`（或 `fts.go` 内部 snapshot cache + `v1` cursor codec） | `internal/personal/store/fts.go:64-71`、`pkg/agentcore/tools/native/ledger.go:26-38` | recall 只有 offset/limit，不提供稳定检索结果集 | 原位扩展仍需进程内状态、TTL、容量、指纹和 codec 的单一 owner | 重跑 SQL 会因新增 entry/排序漂移，无法保证第二页稳定 | store 内部统一持有 TTL、容量、指纹和 codec；对外仍由 `SearchResult` 暴露 | 删除 cursor 功能时恢复 `[]EntryHit` 兼容适配，不迁移数据库 |

### Rejected new tools

| rejected_candidate | capability_id | deletion_test_and_reason |
|---|---|---|
| 用 bm25/时间作为跨页 keyset cursor | CAP-03 | 删除该方案不损失稳定性；设计已明确排序键会漂移，cursor 只保存 snapshot id + offset。 |
| 用 unicode61 替换 trigram 或新增 bigram 表 | CAP-01 | unicode61 连续 CJK 召回不成立，bigram 属 S2；删除不会影响 S1 零迁移目标。 |
| 在 Ledger 中继续 `SessionCwds + pathWithin` 后拼大 IN | CAP-02 | 删除该路径正是 S1 的性能目标；旧实现只作为测试参照，不作为生产适配。 |
| 给 recall 增加 cursor | CAP-04 | recall 已有 offset/limit，删除该扩展保持既有工具面和语义。 |

## 前置检查（preflight）

1. 读取设计 Draft 0.3 的状态行与 Q1–Q8；只有“全部按建议批准”仍成立才开始实施。任何要求做 S2、改变 cursor 失效策略或扩展 CLI 的请求，先回到设计/计划 Gate。
2. 记录 Git root、branch、HEAD、`git status --short` 与目标 worktree；检查 `fts.go`、`ledger.go`、native ledger 文件和 migration 目录无他人改动。发现 owner 冲突即停止。
3. 核对 `entries_fts` migration、`TestFTSExpressionsMatchMigration`、`AppendEntries` 同事务写索引的事实；确认实施不添加 migration、不触碰 rowid 对齐和 GC 语义。
4. 用真实驱动确认本计划依赖的 SQL 形态：`MATCH` 只接收 >=3 rune 的引号化词，LIKE 全部参数化，时间使用 store 的 `timeLayout`，cwd 边界使用绑定参数；不要用 mock 推断查询计划。
5. 盘点 `LedgerSearcher`、`SearchEntries`、`workspaceSessions`、`SessionCwds` 的所有调用者；任何未发现调用者的 API 删除都必须记录，不能因“当前唯一调用点”而顺手清理。
6. **结果排除规则（用户 2026-10-16 已定：两者都排除）**：按现有 `api.Message` 的 `role` 与 `ToolCall.Name`，候选谓词为 `NOT (f.role = 'tool' AND json_extract(e.json, '$.ToolCall.Name') IN ('session_search', 'recall'))`；同时排除 `session_search` 与 `recall`，不额外猜测 `IsError` 条件；由 T1 加入参数化 SQL 与守卫测试，T5 验证工具层结果，未确认不得实施该条件。
7. 运行基线：`go test -race ./internal/personal/store/... ./internal/personal/... ./pkg/agentcore/tools/native/...`；失败先归因，不把基线失败算作本计划完成。

## 波次与依赖

| 波次 | 阶段 | 依赖 | 执行方式 |
|---|---|---|---|
| W0 | 预检、调用者盘点与“自身结果”规则 Gate | 无 | 监督会话；不改代码 |
| W1 | store 按词分流、过滤与 cwd SQL 条件 | W0 | T1 后 T2；同一 fts owner 文件串行 |
| W2 | cursor 编解码、进程内快照与分页 | T1、W1 API 稳定 | T3a → T3b；同一 store owner 串行，避免与 T1/T2 并发改 fts.go |
| W3 | Ledger scope 适配与参数传递 | T2、T3b | T4 |
| W4 | native tool schema/解析/渲染与旧调用兼容 | T3b、T4、W0 Gate | T5；必要时 T6 串行修复接口 |
| W5 | 测试、benchmark、真实库基线与收尾 | T1–T5 | T6、T7 可并行；T8 最终回归 |

## 任务

### T1 按词分流与过滤 SQL（W1）

- 文件：`internal/personal/store/fts.go`、`internal/personal/store/fts_test.go`。
- 动作：保留 `strings.Fields`/字面量转义；将 >=3 rune 词放入引号化 `MATCH`，1–2 rune 词生成参数化 LIKE 条件，同一 WHERE 用 AND；保留有 MATCH 的 bm25 排序、纯短词的 created_at DESC 与 snippet 行为；增加 `Kinds`、`Since`、`Until` 等过滤字段，时间使用半开区间和 store 时间格式；保留 `CWDPrefix` 原语义。经 W0 确认后加入参数化自身结果排除：`AND NOT (f.role = 'tool' AND json_extract(e.json, '$.ToolCall.Name') IN ('session_search','recall'))`，两个名称都作为绑定参数，不迁移 schema。
- 验证命令：`go test -race ./internal/personal/store/...`；覆盖 1/2/3+ rune、中文+英文混合、AND、语法字符、kind 四值/非法、since/until 边界、旧短词结果等价、`session_search`/`recall` tool 结果排除。
- 完成标准：任何进入 MATCH 的 term 都 >=3 rune；混合查询不会返回只有长词的 entry；纯短词与现状召回/排序保持一致；所有新增 SQL 参数化；已确认的自身结果谓词只排除 `role=tool` 且 ToolCall.Name 命中的两类结果。
- 反向验证方法：临时把短词加入 MATCH，带“只有 token、没有中文”的反例必须使守卫测试失败；临时强制全 LIKE，混合查询的索引/分流守卫必须失败；临时删除自身结果谓词，工具结果排除测试必须失败；恢复后重跑。
- 预计改动规模：中（1 个 Go 文件及测试，200–350 行）。

### T2 workspace/current 条件 SQL 下推（W1，依赖 T1）

- 文件：`internal/personal/store/fts.go`、`internal/personal/ledger.go`、`internal/personal/store/fts_test.go`、`internal/personal/ledger_test.go`；仅在调用者盘点后决定是否触及 `sessions.go`。
- 动作：新增 `WorkspaceDir`、`CurrentSessionID`、`FoldCase`（或设计允许的等效结构）；将 current/workspace 条件下推到既有 entries_fts→entries→sessions 查询：当前会话始终包含、非空 cwd 按分隔符边界包含、空 cwd 排除；Windows 按设计使用 lower() ASCII 折叠；Ledger 不再在生产检索路径调用 `SessionCwds`/`pathWithin` 拼 IN；all scope 不加限制。
- 验证命令：`go test -race ./internal/personal/store/... ./internal/personal/...`；使用真实文件库覆盖 `/repo`、`/repo2`、`/repo/sub`、`/repo-旧`、空 cwd、当前会话无 cwd、大小写和 IncludeAgents；cwd fixture 还要覆盖存量非规范化值 `/repo/`、`C:/Repo`、`C:\Repo`，并记录与 Go `filepath.Clean` 参照的任何已知差异。
- 完成标准：新 SQL 命中集合与测试内旧 `pathWithin` 参照实现逐例一致（明确接受非 ASCII lower 差异）；空 SessionIDs 不扩大为 all；EXPLAIN/代码检查确认没有先读全部 cwd 拼大 IN。
- 反向验证方法：临时删除 current-session OR 分支，当前会话无 cwd 用例必须失败；临时恢复旧的 `SessionCwds + pathWithin`，下推路径/SQL 形态守卫必须失败；恢复后重跑。
- 预计改动规模：中至大（2–4 个 Go 文件及测试，220–400 行）。

### T3a cursor 编解码与指纹（W2，依赖 T1/T2）

- 文件：`internal/personal/store/fts_cursor.go`（必要时新增）、store 测试。
- 动作：实现 `v1.<snapshot_id>.<offset>` 的编解码、边界校验与指纹；指纹包含规范化 query、绝对 since/until、scope/current、workspace、kind、IncludeAgents；非法 cursor、坏字段、不同查询条件和未知版本都安全地转为从头查询说明，不进入 SQL。
- 验证命令：`go test -race ./internal/personal/store/...`；覆盖合法/非法/坏字符/未知版本、offset 边界、指纹不匹配与失效说明。
- 完成标准：cursor 只表达 snapshot id + offset，所有不可信输入不影响 SQL；codec 和指纹 owner 单一且无持久化状态。
- 反向验证方法：临时取消固定指纹校验，跨 query/scope/kind cursor 测试必须失败；临时将 cursor 拼入 SQL，参数化/安全守卫必须失败；恢复后重跑。
- 预计改动规模：中（1–2 个 Go 文件及测试，120–220 行）。

### T3b 进程内快照、缓存与分页（W2，依赖 T3a）

- 文件：`internal/personal/store/fts.go`（必要时同包新文件 `fts_snapshot.go`）、store 测试。
- 动作：实现 `SearchResult` 或设计允许的等效结果结构；首次查询建立完整有序 `(session_id, seq, role, snippet, created_at)` 快照，最多保留 500 条；缓存由 `Store` 级互斥锁保护，TTL 10 分钟并以最多 64 个快照的 LRU 做容量上限；按单页 limit 多取一条判断 next cursor，翻页只按有效 snapshot 的 offset 切片，不重新执行 MATCH/bm25/snippet 查询；缓存缺失/重启/TTL/指纹不匹配时从头查询并说明；区分单页上限 100 与快照总量 500，不静默假装结果完整。条目在快照后被删除时，分页仍返回快照命中，后续 recall 自然报告不存在。
- 验证命令：`go test -race ./internal/personal/store/...`；45 条命中连续 `20+20+20` 无重复/遗漏；超过 500 明确提示并只保留前 500；插入/删除后第二页仍按旧快照和首查 snippet；坏 cursor/TTL/缓存缺失/不同 query/scope/kind 从头；并发多个 goroutine 创建、读取、翻页和淘汰快照无 race，超过 64 个快照按 LRU 淘汰。
- 完成标准：有效 cursor 只取既有 snapshot offset；首查结果的 snippet 不因翻页重算而变化；缓存并发安全且有 64 个快照上限；失效 cursor 不报错且结果说明从头；单页与快照上限语义清晰；无持久化状态。
- 反向验证方法：临时让第二页重新执行 SQL，首页后插入一条会改变排序的命中，快照稳定性测试必须失败；临时移除锁或 LRU 上限，并发/race 或容量测试必须失败；恢复后重跑。
- 预计改动规模：中（1–2 个 Go 文件及测试，180–320 行）。

### T4 Ledger scope 与 query 参数传递（W3，依赖 T2/T3b）

- 文件：`internal/personal/ledger.go`、`internal/personal/ledger_test.go`、相关 store 接口调用处。
- 动作：先把 `LedgerSearcher.Search` 的结果接口扩展为可携带 hits、`next_cursor` 和说明行的 `SearchResult`（或等效兼容结构），再将 `current` 映射为 `CurrentSessionID`，workspace 映射为规范化 cwd + current session + FoldCase，all 保持无限制；把 cursor/kind/time 解析后的结构传到 store；保持 scope 缺省语义、空列表绝不当 all、`ref=out:` 逐行扫描与 recall offset/limit 不变。若 `SessionCwds` 无其他调用者，只停用检索路径，不做范围外清理。
- 验证命令：`go test -race ./internal/personal/... ./internal/personal/store/...`；保留 `TestLedgerSearchScopes`、current 无 cwd 回归，并增加 kind/time/cursor 组合和无模型结果链路。
- 完成标准：Ledger 不再把 workspace 全部 session cwd 读入 Go；旧 `scope`、`redact`、`ref=out:` 和 recall 断言继续通过；store 返回的 next cursor/notice 能向工具层传递；接口变更在 T4 完成，T5 只消费稳定结构。
- 反向验证方法：临时把 workspace 传回旧 SessionIDs 路径，SQL 下推/大 IN 守卫必须失败；临时把空 SessionIDs 当 all，空 scope 用例必须失败；恢复后重跑。
- 预计改动规模：中（1–2 个 Go 文件及测试，150–280 行）。

### T5 native session_search schema、解析与结果渲染（W4，依赖 T3b/T4 且受 W0 自身结果规则 Gate 约束）

- 文件：`pkg/agentcore/tools/native/ledger.go`、`pkg/agentcore/tools/native/ledger_test.go`、必要的 native fake/接口测试。
- 动作：`LedgerQuery` 增 `Cursor`、kind 多值、since/until（工具边界解析为规范化值）；schema/description 增可选 `cursor`、`kind`、`since`、`until`，说明 cursor 来自 `next_cursor` 且原样回传，并提示模型优先使用 2 字以上检索词；支持逗号分隔 `user|assistant|tool|compaction`（多值以设计 D10/Q6 为准），相对 `7d/36h` 与日期/RFC3339，since 含/until 不含；带 `ref=out:` 时拒绝 cursor/kind/since/until 组合，避免与逐行扫描语义混合；`renderLedgerHits` 仅在有下一页追加 `next_cursor`，整体继续 `redact.Clean`；不改 recall；按 W0 已确认的唯一规则排除工具自身结果。
- 验证命令：`go test -race ./pkg/agentcore/tools/native/... ./internal/personal/...`；fake searcher 逐项检查参数透传，非法 kind/time 不调用 searcher；首页/末页 next_cursor、cursor 经 redact 不变、旧输出断言和 `ref=out:` 回归通过。
- 完成标准：模型能从结果行取得并原样回传 cursor；新参数都是可选且旧调用不变；非法值和 `ref` 参数冲突 fail-closed；结果文本不泄露游标之外的新敏感字段；已确认的自身结果排除规则有明确测试证据。
- 反向验证方法：临时删除 next_cursor 行或跳过 redact，端到端渲染测试必须失败；临时放宽 kind/time 校验或允许 `ref` 混用，非法参数测试必须失败；临时删除 `session_search`/`recall` 排除断言必须失败；恢复后重跑。
- 预计改动规模：中（1 个 Go 文件及测试，180–320 行）。

### T6 CLI 与存储接口兼容回归（W5，依赖 T4/T5）

- 文件：`internal/cli/sessions.go`、`internal/cli/sessions_test.go`、受 store 返回结构影响的调用适配文件。
- 动作：保持 CLI 的 session 去重、首个 snippet 和现有 `--query/--cwd/--since/--until/--agents/--limit` 行为；按 Q7 不新增 cursor/kind 分页产品面，仅适配 store 新结果结构并确认 CLI 的 session 时间/cwd 语义没有被误改成 entry 过滤。
- 验证命令：`go test -race ./internal/cli/... ./internal/personal/store/...`；现有 CLI JSON/文本断言、中文混合 query 回归。
- 完成标准：CLI 编译并保持既有输出契约；cursor/kind 仅存在于 session_search 工具路径；不引入 CLI 专用第二套检索逻辑。
- 反向验证方法：临时让 CLI 使用 entry 时间/cwd 过滤替代原 session 过滤，既有边界 fixture 必须失败；临时暴露未批准 CLI 参数，接口检查应失败；恢复后重跑。
- 预计改动规模：小至中（1–3 个 Go 文件，80–180 行）。

### T7 测试、benchmark 与真实 sessions.db 基线测量（W5，T1–T6 可分工并行）

- 文件：`internal/personal/store/fts_bench_test.go`（新）、store/ledger/native 测试；测试临时脚本和不提交的真实 DB 副本/测量记录。
- 动作：新增固定种子中英混合语料 benchmark，覆盖 3-rune 选择性/高频、S1 2-rune LIKE、分页第二页；不实现或测量 S2 bigram 作为本期功能。按设计要求在真实 `sessions.db` 副本上只做基线/查询计划与耗时记录，确认不会改原库、不会生成迁移；记录现有库体积与本期无 schema 变化的事实，供未来 S2 另案使用。设计基准第 4 项及验收 (a) 回填耗时、(b) 体积增长属于 S2，不适用于 S1；仍在 1 万/10 万条语料两档记录本期查询基线，不实现 bigram。
- 验证命令：`go test -race ./internal/personal/store/...`；`go test ./internal/personal/store -run '^$' -bench . -benchmem`；用真实驱动执行典型 1/2/3-rune、混合、英文查询的 `EXPLAIN QUERY PLAN` 与实际耗时，并在 1 万/10 万条语料两档运行。
- 完成标准：benchmark 可重复且不把 S2 作为实现承诺；真实 DB 副本测量不污染工作区/生产库；报告包含执行命令、exit code、原始关键输出与已知平台差异；明确标注 S2 的基准第 4 项和验收 (a)(b) 为 S1 不适用。
- 反向验证方法：临时使用随机种子或把真实 DB 路径指向原库，固定种子/安全副本检查必须失败；临时把 2-rune 词放进 MATCH，D3 守卫测试必须失败；恢复后重跑。
- 预计改动规模：中（1 个 benchmark 文件及测试辅助，120–250 行；测量记录不进入生产代码）。

### T8 全量回归、真实 provider 与交付验收（W5，依赖 T1–T7）

- 文件：不新增业务 owner 文件；核对所有测试、benchmark、trace/工具结果和设计范围。
- 动作：窄到宽执行 race、vet、全量测试、staticcheck；完成 store/ledger/native 端到端 fake provider 链路：模型调用 `session_search`，读取 `next_cursor`，原样带 cursor 再调；能使用真实 provider 时做一次 2 字中文查询翻页，不能则记录 `UNVERIFIED_REAL_PROVIDER`；确认无 ptysmoke 需求，因为本期无终端交互改变。
- 验证命令：`gofmt -l $(git ls-files "*.go")`、`go vet ./...`、`go test -race ./...`；主机 `go test -count=1 ./...`、`staticcheck ./...`；端到端测试按包命令执行；真实 provider 命令按环境配置执行。
- 完成标准：所有命令以 exit code 与原始输出共同判定；反向验证成立且临时改动已恢复；S2 没有代码、迁移、索引或计划任务混入；未验证项如实列出。
- 反向验证方法：在临时分支把 MATCH/LIKE 分流守卫、snapshot 偏移或 next_cursor 渲染任一关键检查去掉，确认对应新增测试失败；恢复后只重跑受影响的窄验证再做全量。
- 预计改动规模：小（验证与记录，不应产生额外业务代码）。

## 回滚与恢复

- S1 无迁移、无新表/列/索引、无持久化快照；回滚为按功能点 `git revert`，不需要数据库 DOWN。
- 进程内 snapshot 在进程退出时自然消失；恢复旧代码不会留下需要清理的数据。失效 cursor 本来就从头查询，不依赖旧版本理解新 cursor。
- 若 T3a/T3b/T5 未通过，停止后续波次，不合入半成品；保留独立 worktree 供修复。不得用 reset/stash/checkout 覆盖他人路径。
- 运行时可立即回到旧检索路径的唯一安全方式是回滚代码；不通过新增配置开关扩大本期范围。CLI 旧参数、recall、ref=out 保持兼容。

## 人工 Gate

1. 用户审阅并批准本计划后才开始实施；批准不等于授权 push/发版。
2. W0 记录用户对“排除检索工具自身结果”候选 predicate 的确认；默认建议同时排除 `session_search` 与 `recall`，未确认前不实施该 SQL 条件。
3. T1–T5 合入前由监督会话审核 SQL 全部参数化、MATCH 词长度守卫、cursor 不进 SQL、零迁移事实及 owner diff。
4. T7 的真实 `sessions.db` 只能使用副本；原库不可修改。T8 的真实 provider 核对能做则执行，不能做则由监督方确认未验证记录。
5. 本期不做 S2 bigram；任何要增加 tokenizer、索引、迁移、回填或 CLI 分页的请求都另起设计/计划 Gate。

## 可追溯性

| 设计条目 / 决策 / 待确认定稿 | 任务 | 验证 |
|---|---|---|
| 用户目标 1、D1 不换 tokenizer、D8 不新增索引 | T1、T7 | 1/2/3-rune 与 schema/迁移检查 |
| 用户目标 2、D2 按词分流、D3 短词绝不进 MATCH | T1、T7 | 混合反例、MATCH term 长度守卫、旧 LIKE 等价、自身结果排除 |
| 用户目标 3、D5 cursor 格式、D6 进程内快照、Q4 多取一条 | T3a、T3b、T5 | 45 条分页、snippet 稳定、并发/64 LRU、末页无 cursor、格式/坏 cursor |
| D7 cwd 下推与 lower ASCII 折叠；Q3 接受非 ASCII 差异 | T2、T4 | SQL/参照 pathWithin 等价性与边界 fixture |
| 用户目标 4、kind/时间过滤、D10 多值 kind、D11 时间写法 | T1、T4、T5 | 四 role、逗号值、7d/36h/日期/RFC3339、半开边界 |
| 用户目标 5、D9 recall 不引入 cursor、D13 保留 CWDPrefix | T1、T4、T5、T6 | recall/ref=out/CWDPrefix 旧回归 |
| workspace/current/all scope、IncludeAgents、空 SessionIDs 不能当 all | T2、T4 | scope 组合与 current 无 cwd 测试 |
| native schema/描述、游标原样回传、结果 next_cursor | T4、T5 | SearchResult 接口、native fake 与工具端到端两次调用 |
| redact 保持、游标为纯 token；S1 无迁移 | T5、T7、T8 | redact 等价、migration/schema/diff 检查 |
| Q1 S2 bigram 另案；Q2 2 字英文仍 LIKE；Q8 不增 search 配置 | T1、T7、T8 | diff scope review、2-rune 查询与无新配置检查 |
| Q5 游标过期忽略并说明从头 | T3a、T3b、T5 | TTL/缺失/指纹不匹配测试 |
| Q6 kind 逗号多值；Q7 CLI 不跟进 | T5、T6 | schema/CLI 参数清单与旧 CLI 回归 |
| Q3 lower() ASCII；Q4 只在有下一页返回 cursor | T2、T3a、T3b、T5 | Windows 参数化 fixture、末页断言 |
| “排除检索工具自身结果”范围条目 | W0、T1、T5 | 用户确认 `role=tool` 且 ToolCall.Name 为 `session_search`/`recall` 的谓词；参数化 SQL 与工具层守卫测试 |
| S2 后续前提：真实库体积/性能与迁移边界 | T7（仅基线记录） | 副本上的 EXPLAIN/耗时/体积原始记录，不产生 S2 代码 |
| 非目标：trace/stats、长期记忆、向量、跨库、redact 规则、自动评测 | 全任务 | diff scope review |

### 已定稿的 Q1–Q8

- Q1：S2 bigram 本期不做，另起设计；必须先在真实库量化边界、体积、写放大、并发、rowid 与版本闸门。
- Q2：2 字英文词不纳入未来 CJK-only bigram；S1 仍走 LIKE。
- Q3：接受 Windows `lower()` 的 ASCII 折叠差异，并由测试固定；不引入 SQL 粗筛 + Go 精判。
- Q4：分页多取一条；只有有下一页时返回 `next_cursor`。
- Q5：cursor 过期/不匹配/缺失时忽略，从头查询并加说明行。
- Q6：`kind` 允许逗号分隔多值，合法值为 `user|assistant|tool|compaction`，服务端拒绝未知/空值。
- Q7：CLI 不增加 cursor/kind/分页产品面，仅适配新 store 结果。
- Q8：不增加 `search.*` 配置键；500 条与 10 分钟固定在实现中。

## 待确认 / 风险

- **已定（用户 2026-10-16）**：排除谓词 `role=tool` 且 `ToolCall.Name IN (session_search, recall)`，两者都排除。
- `lower()` 在 Windows 非 ASCII 路径上不完全等价于 Go `strings.ToLower`；按 Q3 接受并固定测试，不扩大为精判方案。
- 快照最多 500 条意味着超过上限的结果不是完整集合；结果必须明确提示，不能把截断称为完整。
- 进程重启/TTL/内存淘汰会使 cursor 从头开始；模型必须依据说明重新翻页。
- `SearchEntries` 结果结构变化会影响 native fake、Ledger 与 CLI；必须串行完成接口适配，不能用第二套旁路 API。
- S1 短词 LIKE 在高命中库上可能慢，这是设计接受的零迁移选择；不要借此偷偷引入 bigram。
- Windows job、真实 provider 和真实 sessions.db 不可用时必须写明原始错误/退出码；不以人工“看起来正确”替代自动证据。

## 完成 Gate

- T1–T8 的完成标准全部满足，W0 的自身结果规则 Gate 已解决或设计明确撤回该条范围。
- 每个任务均完成反向验证，且临时破坏已恢复；S2 未混入代码、迁移、索引、配置、benchmark 承诺；S1 的 T7 明确记录 1 万/10 万条基线，且 S2 基准第 4 项与验收 (a)(b) 标为不适用。
- 容器 `gofmt`/`go vet`/`go test -race ./...` 与主机 `go test -count=1 ./...`/`staticcheck ./...` 的结果均按 exit code 和原始输出记录；真实 provider/Windows 未验证项单独列出。
- `git status --short` 确认无夹带；每个功能点单独 conventional commit，仅 `git add` 对应计划/实施 owner 文件，不提交 `.gofer/tracker`。
- 任何需要改变已批准协议语义、增加迁移或扩大 S2 范围的发现，停止实施并回到设计/计划 Gate。
