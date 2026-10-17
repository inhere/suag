<!-- template_id: plan; template_version: 1.2.0 -->
# 记忆检索与交接改进实施计划

> 状态：Draft 0.2 / 已批准实施

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-17 | suga（gofer job） | 依据 `docs/design/2026-10-17-memory-search-handoff-design.md` Draft 0.1，按三件事拆成可分别实施与验收的任务（T0–T6），含反向验证、跨平台与真实模型 Gate。 |
| 0.2 | 2026-10-18 | 监督（Claude） | 用户批准实施（2026-10-18“记忆检索的设计按推荐，批准”）：Q1–Q5 按建议结论执行。 |

## 目标与完成定义

目标：实现设计 0.1 的三件事——files 后端的记忆检索改走可重建的 FTS5 派生索引并按相关度排序；交接摘要改固定小节并进 `session_search`；记忆条目增加可选 `applies_when`。

完成定义：

1. `memory search` 对 files 后端按 bm25 相关度返回，1–2 rune 的词仍能命中；条目文件被手改后下一次搜索即反映新内容；索引缺失/损坏/未绑定会话库时回退到子串搜索且不报错。
2. `MEMORY.md` 与条目文件仍是唯一真源：删掉索引表或整个 `<SUGA_HOME>` 下的库后，下一次搜索自动重建，无需命令。
3. 交接摘要（分段交接与 `--from` 交接）使用固定小节；`session_search(kinds=handoff)` 能搜到历史与新增的 handoff 摘要，`recall` 能读原文。
4. 记忆条目可带 `applies_when`：`memory get` 展示、索引行不展示、`files`/FTS 搜索可命中；旧条目（无该字段）与旧后端行为不变。
5. 每件事有近端单测（`github.com/gookit/goutil/x/assert`、`t.Run` 多用例）与至少一次反向验证（去掉被测逻辑后对应检查失败）。
6. Windows 主机命令与 Linux 容器 `-race` 全部 exit code=0；真实模型一轮验证新的摘要模板确实产出固定小节（做不到时在交付说明里写 `UNVERIFIED_REAL_MODEL`）。
7. 每个功能点原子本地提交；不 push、不打 tag、不改 `.gofer/tracker`。

## 范围、排除项与授权

- 范围：`internal/personal/store`（`memory_fts` 迁移、同步与查询）、新增 `internal/adapters/memindex`、`pkg/agentcore/memory`（`Enumerator`、`AppliesWhen`）、`pkg/agentcore/memory/files`（`Entries`、frontmatter、搜索文本）、`pkg/agentcore/tools/native`（memory schema/渲染、session_search kinds）、`internal/wiring`（装配与迟到绑定）、`internal/personal`（`compactTemplate`）、`internal/personal/store/fts*.go` 与新迁移、对应测试。
- 排除项：向量/embedding/混合检索；gofer 后端与 exec 协议的检索行为；`session_search` 的分流与打分；交接包的 JSON 解析；记忆衰减与自动过期；`suga memory reindex` 等新命令；chat 渲染与 trace 事件。
- 兼容边界：`memory.Store` 的四个方法签名不变；exec 协议版本仍为 1（只加可选字段）；`entries_fts` 的 MATCH/LIKE 分流与 bm25 行为不变（只加被索引的类型）；稳定系统提示词与段上下文组成不变。若要改动这些语义，停止并回到设计 Gate。
- `host_or_non_offline_action=REQUIRED`：Linux 容器的 `go test -race ./...`、Linux 侧的伪终端/真实二进制验证、以及一次真实 provider 的摘要模板验证都属于实施完成 Gate 的外部环境动作，计划阶段不执行；Windows 主机只能跑 build/vet/gofmt/test/staticcheck，不能跑 `-race` 与 ptysmoke。
- 授权：用户批准设计与本计划后才实施；批准仅授权当前 worktree 的本地修改与提交，不授权 push/tag/release/tracker 修改。

## 输入与批准证据

- 设计：`docs/design/2026-10-17-memory-search-handoff-design.md`，Draft 0.1 / 待用户审批。
- issue：`suga-xis4`（memory search 相关度）、`suga-fuw0`（交接摘要结构化并进 FTS）、`suga-8nzb`（`applies_when`）；来源 `local/analysis/2026-10-17-hy-memory-zvec-exploration.md` A/C 节（`tmp` 内仅供阅读，未入库）。
- 记忆检索现状：`pkg/agentcore/memory/files/files.go:43,50-51,120-138,248-310,432-469`。
- FTS 现状：`internal/personal/store/fts.go:14-72,141-258,300-370`、`internal/personal/store/fts_cursor.go:210-229`、`internal/personal/store/entries.go:15,53`、`internal/personal/store/migrations/20261010-234359-add-ledger-fts-memory-meta.sql:12-43`、`internal/personal/store/fts_test.go:355-397`、`internal/personal/store/store.go:21-22,117,124-181`。
- 装配现状：`internal/wiring/memory.go:158-175,177-200,202-277`、`internal/wiring/memory_runtime.go:77-118,143-170`、`internal/wiring/runtime.go:244,487-489,551,648`、`internal/wiring/manifest.go:400`、`pkg/agentcore/layering_test.go:50-52,61,88-123`。
- 交接现状：`internal/personal/compact.go:28-46,363-378,446-469`、`internal/personal/segment_handoff.go:18-36,63-83,216-259,274-286`、`internal/personal/handoff.go:18-25,94-152`、`internal/personal/context.go:82-96,128-130,163-171,186`。
- 条目格式与工具契约：`pkg/agentcore/memory/types.go:121-153,191-201`、`pkg/agentcore/memory/files/index.go:154-187,189-228`、`pkg/agentcore/tools/native/memory.go:24-27,68-115,246-253,343-365`、`pkg/agentcore/memory/execstore/execstore.go:6-30,86-123,224-238`、`internal/adapters/gofermem/gofermem.go:388-424`。
- 实施前必须重新确认 HEAD、branch、dirty 状态；代码事实与设计不一致时停止，不进入 T1。

## Capability Discovery

### Capability decisions

| capability_id | required_capability | searched_candidates | direct_reuse | thin_adapter_or_owner_extension | decision | proven_gap | duplication_and_lifecycle_risk |
|---|---|---|---|---|---|---|---|
| CAP-01 | 记忆条目的 FTS5 索引存储、增量同步与相关度查询 | `internal/personal/store/fts.go:141-258,311-370`；`migrations/20261010-234359-add-ledger-fts-memory-meta.sql:12-43`；`store.go:124-181` | fts5 trigram 表、`ftsMatchExpr`、`escapeLike`、`likeSnippet`、`minMatchRunes`、`bm25`/`snippet` 用法、迁移机制 | `internal/personal/store` 新增 `memfts.go` 与一个建表迁移 | MINIMAL_NEW_MODULE | 现有表只服务会话底账，没有记忆文本的存放位置 | 查询逻辑只在同包复用，不导出不改 `entries_fts`；新表由迁移拥有，可整表重建 |
| CAP-02 | 批量枚举 files 后台条目（标题/正文/kind/updated） | `pkg/agentcore/memory/files/files.go:248-310`；`pkg/agentcore/memory/store.go:15-25` | `files.Search` 里已有的目录扫描与 `readEntry` | `pkg/agentcore/memory` 新增可选接口 `Enumerator`，`files` 实现 | THIN_ADAPTER | `Store` 只有 Index/Get/Write/Search，无法批量读正文 | 只是可选接口，其他后端不必实现；不改变 `Store` 契约与已有调用点 |
| CAP-03 | 把 files 后端包装成“索引优先、子串回退”的 Store | `internal/wiring/memory.go:158-200`；`internal/wiring/memory_runtime.go:143-170`；`internal/wiring/runtime.go:487-489,648`；`internal/adapters/gofermem` 先例 | `memory.Store`/`Router`/`Governed`、`memoryHost` 的迟到绑定模式 | 新增 `internal/adapters/memindex` 包装器 + `MemorySetup` 暴露绑定点 | MINIMAL_NEW_MODULE | 现有装配无法持有会话库；也没有“派生索引”这一层 | 唯一知道派生索引的地方；未绑定时行为等于现状；不让 wiring 复制查询逻辑 |
| CAP-04 | 交接摘要固定小节 | `internal/personal/compact.go:37-46`；`internal/personal/segment_handoff.go:216-259,274-286` | `compactTemplate`、`renderPackage`、`refsHeading`/`parseRefs` | 只改模板字符串与模板外的固定说明 | DIRECT_REUSE | 现有小节不含坐标/采纳与否决/验证到哪 | 不新增解析器；`refsHeading` 不变，`parseRefs` 与 `markdownSection` 不受影响 |
| CAP-05 | handoff 摘要进入会话 FTS 与 `session_search` | `internal/personal/store/fts.go:27-63,300-309`；`fts_cursor.go:210-229`；`pkg/agentcore/tools/native/ledger.go:101-102,126,241-258`；`fts_test.go:355-397` | `ftsIndexedTypes`/`ftsTextExpr`/`ftsSourceSelect`、kinds 白名单、`SearchEntriesPage` | 常量与表达式扩一项 + 新迁移回填 + kinds 白名单与工具描述扩一项 | OWNER_EXTENSION | handoff 记录只有 `$.summary`，走 ELSE 分支文本为空 | 常量与迁移双份拷贝由漂移测试守住；只新增 role，不改既有 role 行为 |
| CAP-06 | 记忆条目可选 `applies_when` | `pkg/agentcore/memory/types.go:121-153`；`pkg/agentcore/memory/files/index.go:154-228`；`pkg/agentcore/tools/native/memory.go:90-115,343-349`；`execstore.go:86-123` | 扁平 frontmatter 手写解析器、工具 schema 与渲染函数、exec wire 类型 | 三层各加一个可选字段 | OWNER_EXTENSION | 条目没有适用条件字段；索引行/intent 也无法表达 | 不进 `MEMORY.md`（不占预算）；旧条目/旧二进制互相忽略；exec 版本不变 |

### New module candidates

| candidate_id | capability_id | proposed_module | searched_candidates | direct_reuse_gap | thin_adapter_or_owner_extension_gap | proven_gap | unique_owner_and_lifecycle | deletion_or_merge_handling |
|---|---|---|---|---|---|---|---|---|
| MOD-01 | CAP-01 | `internal/personal/store/memfts.go` | `fts.go`、`fts_snapshot.go`、`memory_meta.go` | 没有记忆文本的索引表与同步/查询入口 | 只加表与两个方法，复用同包辅助 | `memory_fts` 不存在 | 记忆索引的 DDL 与查询的唯一 owner | 删除后 `memindex` 回退子串；DOWN 迁移删表，不留数据 |
| MOD-02 | CAP-03 | `internal/adapters/memindex` | `internal/adapters/gofermem`、`internal/wiring/memory.go`、`memoryHost` 绑定 | 装配处没有会话库，无法在 `buildMemory` 内完成绑定 | 一个包装器 + 一个 `Bind` | 没有“索引优先”的 `memory.Store` 实现 | 派生索引读写的唯一 owner；与 `files` 后端生命周期一致 | 删除后 Router 直接持有 files 后端，搜索回到子串 AND |

### Rejected new tools

| rejected_candidate | capability_id | deletion_test_and_reason |
|---|---|---|
| 向量索引或 embedding 客户端（zvec/onnx） | CAP-01 | 删除后 FTS5 已覆盖词面检索；引入原生库会破坏单二进制与 Windows 支持（调研 4.2、5.3）。 |
| 独立的记忆索引文件（JSON/SQLite sidecar） | CAP-01 | 删除后会话底账同一库内的 `memory_fts` 已能表达；多一个文件就多一套备份/清理/并发生命周期。 |
| `suga memory reindex` 命令 | CAP-01 | 删除后每次搜索的增量同步与迁移回填已足够；命令增加一个必须解释的状态。 |
| gofer/exec 后端的检索改造 | CAP-03 | 删除后 gofer 仍走 `gofer memory ls` + 本地子串、exec 仍由外部命令负责搜索；改造收益小且会改协议语义。 |
| 交接包 JSON 结构解析器 | CAP-04 | 删除后固定小节文本已可被模型与 FTS 消费；解析器会让手改与旧记录失效。 |
| 单独的 `handoff_fts` 表 | CAP-05 | 删除后 `entries_fts` 加一个类型即可；独立表要复制移除/分页/工作区过滤逻辑。 |
| `applies_when` 的版本比较与过期标记 | CAP-06 | 删除后字段本身已让模型自行判断；版本解析脆弱且无权威来源。 |

## 前置检查

- P0：记录 `git status --short`、HEAD、branch（当前 `docs/memory-search-handoff-design`，基于 `acd25cfb` = v0.29.0）；每次提交前核对 `git diff --stat` 与 `git diff --cached --name-only`。
- P1：重读设计 0.1 与三个 issue，确认方案未变；确认 `memory_fts`、`Enumerator`、`applies_when` 在代码中均不存在（grep）。
- P2：基线：`go test -count=1 ./pkg/agentcore/memory/... ./internal/personal/... ./internal/wiring/... ./internal/adapters/...`，记录 exit code；失败标 `BASELINE_FAILED`，不进入 T1。
- P3：确认 `modernc.org/sqlite` 下 fts5 trigram 可用（`entries_fts` 已在用），并在临时库上验证 `DELETE ... WHERE scope=?` 对 UNINDEXED 列可用、`bm25()` 在单文本列上的排序方向。
- P4：确认 `MemorySetup` 可以携带适配器且绑定点唯一（`internal/wiring/runtime.go:648`）；确认 `suga manifest` 未绑定路径（`internal/wiring/manifest.go:400`）只读不搜索。
- P5：确认 `TestFTSExpressionsMatchMigration`（`internal/personal/store/fts_test.go:355-397`）在新迁移下的改法：按顺序取所有含回填标记的迁移，最后一个必须与当前常量一致；确认 `schema_migrations` 行数断言（`fts_test.go:341`）不受新增迁移影响。

## 波次与依赖

| 波次 | 任务 | 依赖 | 执行方式 |
|---|---|---|---|
| W0 | T0 前置检查与 Gate | 无 | 只读；基线失败即停止 |
| W1 | T1 记忆 FTS5 派生索引与相关度检索 | T0 | 串行：store → memory/files → adapter → wiring，一次提交 |
| W2 | T2 交接摘要固定小节 | T0（与 T1 无代码交集） | 可并行；先模板单测 |
| W3 | T3 handoff 摘要纳入 FTS | T2（模板决定小节，T3 只加索引） | 串行：常量 + 迁移 + kinds + 工具描述 |
| W4 | T4 记忆条目 `applies_when` | T1（索引路径要包含新字段） | 串行：types → files → native → execstore |
| W5 | T5 跨平台与反向验证 Gate | T1–T4 | 容器 `-race`、Windows 主机命令、真实模型 |
| W6 | T6 文档与使用说明同步 | T1–T4 | 只改 README/技能参考的相关段落 |

## 任务

### T0 前置检查与实现 Gate

- 文件：无产品改动；证据写入交付报告。
- 动作：执行 P0–P5；核对设计 Q1–Q5；确认绑定点、迁移改法、fts5 行为与基线。
- 验证命令：`go test -count=1 ./pkg/agentcore/memory/... ./internal/personal/... ./internal/wiring/... ./internal/adapters/...`；`git status --short`、`git log --oneline -1`。
- 完成标准：基线 exit code=0；无 owner 冲突；Q1–Q5 结论写进交付说明。代码事实冲突则停止，不进入 T1。
- 反向验证：不适用（T0 失败即 fail-closed）。
- 预计改动规模：无。
- 提交：不提交。

### T1 记忆 FTS5 派生索引与相关度检索

- 文件：`internal/personal/store/migrations/20261017-*-add-memory-fts.sql`（新）、`internal/personal/store/memfts.go`（新）及测试；`pkg/agentcore/memory/store.go`（可选接口 `Enumerator`）、`pkg/agentcore/memory/files/entries.go`（新，`Entries` 实现，复用现有扫描）、`pkg/agentcore/memory/files/files_test.go`；`internal/adapters/memindex/`（新包 + 测试）；`internal/wiring/memory.go`、`internal/wiring/memory_runtime.go`、`internal/wiring/runtime.go`、`internal/wiring/memory_test.go`（或等价测试）；`pkg/agentcore/tools/native/memory.go`（描述修正）。
- 动作：建 `memory_fts(scope UNINDEXED, entry_id UNINDEXED, text, content_hash UNINDEXED, updated UNINDEXED, tokenize='trigram')` 与 DOWN；`SyncMemoryIndex(ctx, scope string, srcs []MemorySource) (added, removed int, err error)`（单事务，sha256 比对，先删后插）；`SearchMemoryIndex(ctx, scope, q, limit)`（≥3 rune 走 MATCH + bm25 + snippet，1–2 rune 并上 LIKE，无长词时整条 LIKE 按 updated 倒序）；`memory.Enumerator` + `files.Store.Entries`；`memindex.Store`（`Bind`、`Search` 同步+查询+按枚举结果补 title/kind、未绑定或出错委托底层、`Index/Get/Write` 委托、互斥锁）；`buildMemory` 对 files 后端包一层并把适配器放进 `MemorySetup`，`runtime.go:648` 处 `Bind(repo)`；`memory` 工具描述修正“索引在会话开始时冻结”的过时说法。
- 验证命令：`go test -count=1 ./pkg/agentcore/memory/... ./internal/adapters/memindex/... ./internal/personal/store/... ./internal/wiring/...`。
- 测试要点（每条 `t.Run`）：标题命中排在正文偶发命中之前（bm25）；英文 3+ rune 与中文 2 字查询都能命中；`applies_when` 暂不涉及（T4 加断言）；手改条目文件后再次搜索命中新词、删除文件后不再命中；删掉库文件后重建；未 `Bind` 时结果等于底层子串搜索；`Write` 后立即 `Search` 能看到新条目。
- 完成标准：新函数都有实际调用点（`SyncMemoryIndex`/`SearchMemoryIndex` 只被 `memindex` 调用并有 grep 证据）；测试不碰真实 `~/.suga`（`t.TempDir()`）；`suga manifest` 路径不受影响。
- 反向验证：临时让 `Search` 跳过同步 → 手改用例失败；临时把排序换成 `updated DESC` → 相关度用例失败；临时去掉未绑定回退（返回错误）→ 回退用例失败。
- 预计改动规模：L，8–12 个 Go/测试/迁移文件。
- 提交：`feat(memory): index files entries in fts5 and rank search hits`。

### T2 交接摘要固定小节

- 文件：`internal/personal/compact.go`（`compactTemplate` 与模板外固定说明）、`internal/personal/compact_test.go`、`internal/personal/segment_handoff_test.go`（如已存在则补断言）。
- 动作：把模板改为固定小节：`## 当前任务`、`## 坐标（文件 / 符号 / 分支 / 版本）`、`## 采纳与否决`、`## 验证到哪`、`## 进展与阻塞`、`## 下一步`、`## 与当前任务相关的引用`（最后一个必须与 `refsHeading` 逐字一致）；加三条硬约束（给定量原样保留；计划未执行不得写成已执行；局部搜索结果不得夸大为“不存在”）；`renderPackage` 与 `packagePlanHeading` 不变。
- 验证命令：`go test -count=1 ./internal/personal/...`。
- 测试要点：`compactTemplate` 含全部固定小节且以 `refsHeading` 结尾；摘要请求正文包含模板与三条约束；`parseRefs` 对沿用旧模板的历史摘要仍能取到 ref（向后兼容）；`renderPackage` 输出的小节顺序与旧实现一致（请求/插话/计划追加）。
- 完成标准：段上下文与稳定系统提示词无改动（相关断言不变）；旧摘要记录仍可正常回放（`context.go` 用例通过）。
- 反向验证：临时删掉 `## 验证到哪` 小节 → 模板断言失败；临时把 `refsHeading` 改成新字符串 → `parseRefs` 兼容用例失败。
- 预计改动规模：S，2–4 个 Go/测试文件。
- 提交：`feat(personal): structure handoff summaries into fixed sections`。

### T3 handoff 摘要纳入 FTS

- 文件：`internal/personal/store/fts.go`、`internal/personal/store/fts_cursor.go`、`internal/personal/store/migrations/20261017-*-index-handoff-fts.sql`（新）、`internal/personal/store/fts_test.go`、`pkg/agentcore/tools/native/ledger.go` 及测试、`internal/personal/ledger.go`（如需 kinds 映射）。
- 动作：`ftsIndexedTypes` 加 `'handoff'`；`ftsTextExpr` 加 `WHEN type = 'handoff' THEN trim(json_extract(json,'$.summary'))` 分支（或在 compaction 分支上用 `type IN ('compaction','handoff')`）；新增迁移回填现有 handoff 行并写 DOWN（删 `role='handoff'` 的索引行）；`validateSearchKinds` 与 `canonicalSearchKinds` 加 `EntryHandoff`；native `session_search` 的 kinds 描述与允许集合加 `handoff`；`TestFTSExpressionsMatchMigration` 改为按顺序比对所有含回填标记的迁移，最后一份必须与当前常量一致。
- 验证命令：`go test -count=1 ./internal/personal/... ./pkg/agentcore/tools/native/...`。
- 测试要点：写 handoff 条目后 `SearchEntries` 命中且 `Role=="handoff"`；迁移回填历史 handoff 行（老库 fixture：迁移到旧版本 → 写 handoff → 迁移到新版本 → 行数与内容一致）；`kinds=handoff` 只返回 handoff；`kinds=compaction` 不受影响；不带 kinds 时两种 role 都能出现；`session_search`/`recall` 自身的转录仍被排除。
- 完成标准：漂移测试通过（常量与最新迁移一致）；`schema_migrations` 行数断言仍成立；旧库升级后能搜到历史交接摘要。
- 反向验证：临时移除 `'handoff'` 类型 → 新写入的 handoff 搜不到，用例失败；临时去掉迁移回填 → 老库 fixture 失败；临时把 handoff 加进 kinds 但去掉白名单校验 → 非法 kind 用例失败。
- 预计改动规模：M，5–7 个 SQL/Go/测试文件。
- 提交：`feat(store): index handoff summaries for session search`。

### T4 记忆条目可选 `applies_when`

- 文件：`pkg/agentcore/memory/types.go`、`pkg/agentcore/memory/types_test.go`、`pkg/agentcore/memory/files/index.go`、`pkg/agentcore/memory/files/files.go`、`pkg/agentcore/memory/files/files_test.go`、`pkg/agentcore/tools/native/memory.go` 及测试、`pkg/agentcore/memory/execstore/execstore.go` 及测试、`internal/adapters/memindex`（索引文本包含该字段的断言）。
- 动作：`Entry.AppliesWhen []string` 与 `Op.AppliesWhen []string`（`ValidateText` 加约束：每条单行、≤200 rune、最多 10 条）；`renderEntry` 逐行写 `applies_when: "<v>"`，`parseEntry` 重复键追加；`files.Search` 的匹配文本追加 `applies_when`；`memory` 工具 schema 增 `applies_when`（array of string）与解析，`get` 渲染追加 `applies_when: a · b`，`add/replace` 结果行不变；exec `wireEntry`/`wireOp` 加可选 `applies_when` 并更新协议文档注释，版本仍为 1；`memindex` 组装文本时包含该字段。
- 验证命令：`go test -count=1 ./pkg/agentcore/memory/... ./pkg/agentcore/tools/native/... ./internal/adapters/memindex/...`。
- 测试要点：新条目写入后 frontmatter 含多行 `applies_when`、`Get` 读回一致；旧条目文件（无该字段）读回空切片且不报错；手写单引号/无引号值可读；超过 10 条或含换行报 `ErrInvalid`；`search` 用 `applies_when` 里的词能命中（files 子串路径与 FTS 路径各一例）；`memory get` 输出含该字段、索引行不含（`IndexLine.Render` 断言）；exec 往返透传（fixture 命令回显字段）。
- 完成标准：`MEMORY.md` 行格式与 `Index.Bytes` 不变（预算用例仍通过）；未提供字段时行为与现状逐字节一致。
- 反向验证：临时让 `parseEntry` 忽略该键 → 往返用例失败；临时让 `memindex` 组装文本不含该字段 → FTS 命中用例失败；临时让 `IndexLine.Render` 显示该字段 → 预算/索引行断言失败。
- 预计改动规模：M，6–9 个 Go/测试文件。
- 提交：`feat(memory): add optional applies_when to memory entries`。

### T5 跨平台与反向验证 Gate

- 文件：无功能改动（必要时只补测试）。
- 动作：在 Windows 主机跑 build/vet/gofmt/test/staticcheck；Linux 容器跑 `-race`；用真实 provider 做一轮摘要模板验证（构造一次段切换或 `:compact`，检查摘要含固定小节）；逐项做 T1–T4 的反向验证并记录日志；ptysmoke 不涉及新的交互行为，若实施中发现需要（例如 chat 显示摘要小节），才补场景并注明“未运行，待 Linux 容器验证”。
- 验证命令：主机 `go build ./...`、`go vet ./...`、`gofmt -l $(git ls-files "*.go")`（无输出）、`go test -count=1 ./internal/... ./pkg/...`、`staticcheck ./...`；容器 `go test -race ./...`；真实模型轮次见上。
- 完成标准：所有命令 exit code=0 且输出无新告警；反向验证有“去掉逻辑后失败、恢复后通过”的证据；无法执行的动作写 `UNVERIFIED_REAL_MODEL` / `UNVERIFIED_PTY`。
- 反向验证：即本任务主体（T1–T4 各至少一项）。
- 预计改动规模：S（仅测试）。
- 提交：如需补测试 `test(memory): cover fts index and applies_when edge cases`，无改动则不提交。

### T6 文档与使用说明同步

- 文件：`README.md`（Memory 与 Sessions 两节的相应段落）、`skills/suga-guide/references/sessions.md`（若其中描述了交接摘要格式）。
- 动作：说明 `memory search` 按相关度排序、索引可从 Markdown 重建（删库/手改后自动恢复）；说明交接摘要的固定小节与 `session_search(kinds=handoff)` 用法；说明 `applies_when` 的写法与用途。不改已发布的 CHANGELOG 段落（本计划不改代码版本行为，若实施时改了用户可见行为才在「未发布」下加一条）。
- 验证命令：`git diff --stat`；人工核对文档与实现一致（grep 新小节标题、`kinds` 取值）。
- 完成标准：文档与实现无出入；不引入新术语体系。
- 反向验证：不适用（纯文档）。
- 预计改动规模：S，1–2 个 Markdown 文件。
- 提交：`docs: document memory fts search and handoff sections`。

## 提交与合并顺序

获批后按功能点本地提交，不 push：

1. `feat(memory): index files entries in fts5 and rank search hits`（T1）
2. `feat(personal): structure handoff summaries into fixed sections`（T2）
3. `feat(store): index handoff summaries for session search`（T3）
4. `feat(memory): add optional applies_when to memory entries`（T4）
5. `test(memory): ...`（T5，仅当补测试）
6. `docs: document memory fts search and handoff sections`（T6）

每次提交前执行 `git status --short`、`git diff --stat`、`git diff --cached --name-only`，只 `git add` 当前功能文件；不改 `.gofer/tracker`。T0/T5 的 external gate 不制造空提交。

## 回滚与恢复

- T1 可独立 revert：Router 恢复直接持有 files 后端，搜索回到子串 AND；`memory_fts` 表残留无害，DOWN 迁移可删表。适配器回退分支保证“部分回滚”（例如只回滚 wiring）也不会让搜索报错。
- T2 可独立 revert：模板字符串恢复旧小节；已写入的摘要记录不受影响（FTS 只存文本）。
- T3 可独立 revert：从常量和 kinds 白名单移除 `handoff`，新增迁移的 DOWN 删除 `role='handoff'` 的索引行；`compaction` 行为不变。
- T4 可独立 revert：旧二进制读新条目会忽略 `applies_when` 行；新二进制读旧条目得到空切片；exec 协议的可选字段对旧命令是未知字段（被忽略）。
- 用户数据恢复：`memory_fts` 与 `entries_fts` 的 handoff 行都是派生数据，可删除后重建/回填；不迁移、不改写用户 Markdown。

## 人工 Gate

1. Gate A：用户批准设计与本计划；批准前只保留两份 Draft 文档的提交。
2. Gate B：T0 确认绑定点、迁移改法与 fts5 行为；若需要改 `memory.Store` 语义、exec 协议版本或段上下文，暂停。
3. Gate C：T1–T4 的近端测试与反向验证通过后才跑 T5 的外部命令与真实模型。
4. Gate D：监督在 Linux 容器验证 `-race` 与（如新增）PTY 场景，在 Windows 主机核对命令输出；真实模型证据齐全后才宣布完成。不得 push/tag/release。

## 可追溯性

| 设计要求 | 计划任务 | 证据 |
|---|---|---|
| D1/D2 派生索引与同步 | T1 | 迁移、同步单测、手改/删库用例、未绑定回退用例 |
| D3 复用 session_search 的 trigram/bm25 | T1 | 短词与长词用例、排序用例；同包复用（无复制实现） |
| D4 gofer/exec 不改 | T1/T4 | gofermem/execstore 既有测试不变；exec 只加可选字段 |
| D5 `Enumerator` 可选接口 | T1 | `files.Store` 实现断言、其他后端不实现时回退 |
| D6 迟到绑定与降级 | T1 | 绑定点 grep 证据、未绑定用例 |
| D7 交接固定小节 | T2 | 模板断言、真实模型一轮（或 `UNVERIFIED_REAL_MODEL`） |
| D8 handoff 进 `entries_fts` | T3 | 漂移测试、老库回填 fixture、`kinds=handoff` 用例 |
| D9 `applies_when` frontmatter | T4 | 往返/兼容/预算用例、两条搜索路径各一例 |
| D10 失败降级 | T1 | 注入错误的用例（同步失败仍返回子串结果） |
| D11 不加 trace/渲染改动 | T1–T4 | diff review：无 `Event*` 新增、无 chat 渲染改动 |

## 完成 Gate

宣布完成必须同时满足：

- 文档 validator：`python -I D:\work\inhere\runtime\idev-std-releases\0.26.1\plugins\inhere-dev-standards\skills\inhere-doc-workflow\scripts\validate_document.py --kind design docs/design/2026-10-17-memory-search-handoff-design.md` 与 `--kind plan docs/plans/2026-10-17-memory-search-handoff-plan.md` 均 exit code=0、`errors=[]`。
- Windows 主机：`go build ./...`、`go vet ./...`、`gofmt -l $(git ls-files "*.go")`（无输出）、`go test -count=1 ./internal/... ./pkg/...`、`staticcheck ./...` 均 exit code=0。
- Linux 容器：`go test -race ./...`、`go vet ./...` 通过；如新增 PTY 场景则 `make smoke-chat` 通过并有正反向日志（否则写 `UNVERIFIED_PTY`）。
- 真实模型：至少一轮验证摘要模板产出固定小节，或用 `suga trace --json` 展示摘要文本；做不到写 `UNVERIFIED_REAL_MODEL`。
- 反向验证：T1–T4 各至少一项“去掉逻辑后检查失败”的证据。
- `git status --short` 无夹带，提交列表仅 T1–T4（及可选 T5/T6）对应功能点；不 push。
