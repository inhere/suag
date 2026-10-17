<!-- template_id: design; template_version: 1.1.1 -->
# 记忆检索与交接改进设计

> 状态：Draft 0.1 / 待用户审批

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-17 | suga（gofer job） | 依据 `suga-xis4`、`suga-fuw0`、`suga-8nzb` 与 `local/analysis/2026-10-17-hy-memory-zvec-exploration.md` 的 A/C 节，设计 files 后端的 FTS5 派生索引、交接摘要固定小节并纳入 FTS、记忆条目可选 `applies_when`。 |

### 思考模式与范围冻结

- thinking_mode：RIGOROUS。
- core_objective：让记忆检索按相关度返回（不再只按更新时间倒序），让“上次做到哪”能跨会话被 `session_search` 搜到，让依赖版本/环境的经验可以被判断是否过期。三者都不引入向量检索、不改变 `MEMORY.md` 为唯一真源、不改变段与提示词缓存的既有规则。
- allowed_scope：`pkg/agentcore/memory`（Entry/Op/可选枚举接口）、`pkg/agentcore/memory/files`（frontmatter、按相关度搜索文本）、`pkg/agentcore/tools/native`（memory 工具 schema/描述、session_search kinds）、`internal/personal/store`（新 FTS 表、迁移、查询）、新增 `internal/adapters/memindex`、`internal/wiring`（装配与迟到绑定）、`internal/personal`（交接模板）、对应测试与文档。
- non_goals：向量/embedding；gofer/exec 后端的检索改造；自动记忆抽取或衰减；交接包的机器可解析 JSON；`session_search` 的排序或分词改动；新的 CLI 子命令。
- expansion_policy：DEFER_OR_REQUEST。若要改变 `memory.Store` 既有四个方法的语义、exec 协议版本、段上下文/稳定系统提示词、或 `entries_fts` 的既有打分行为，停止实施并回到设计 Gate。
- review budget：小。三件事彼此独立、各自可单独 revert；每件事只复用已有机制（FTS5 trigram、`memory.Store` 接口、frontmatter 手写解析器、交接模板字符串）。
- 停止条件：用户批准本设计与对应计划前不实施。

## 背景与目标

用户可见痛点有三个，来自同一份调研（Hy-Memory 借鉴点）与三个 issue：

1. `memory search` 现在是子串 AND 匹配、不打分，只按更新时间倒序（`pkg/agentcore/memory/files/files.go:248-310`）。条目多在 10–200 条之间，标题命中与正文偶发命中排在同一序位，模型难以判断先读哪条。
2. 交接摘要（分段交接包、`--from` 交接）是自由文本，而且 handoff 条目**不进** `entries_fts`（`internal/personal/store/fts.go:32`），跨会话搜“上次做到哪”搜不到。
3. 记忆条目没有“依赖的版本/环境”这类适用条件字段（files frontmatter 只有 kind/title/updated/source/session，`pkg/agentcore/memory/files/index.go:154-187`），依赖升级后无法判断某条经验是否还成立。

目标（每件事独立验收）：

1. files 后端的 `Search` 改走可随时重建的 FTS5 派生索引，按 bm25 相关度排序；索引与 `MEMORY.md`/条目文件不一致时以文件为准（含手改）。
2. 交接摘要改为固定小节（坐标 / 采纳与否决 / 验证到哪 / 阻塞 / 下一步），并让 `session_search` 能搜到 handoff 摘要；模板只影响摘要请求，不影响段上下文与稳定前缀。
3. 条目 frontmatter 增加可选 `applies_when`（可多行），`memory get` 展示、索引行不展示、搜索可命中；旧条目与旧后端不受影响。

## 范围与非目标

### 范围

- `internal/personal/store`：新表 `memory_fts` 与迁移、同步与查询 API（复用现有 trigram/短词/bm25 辅助）。
- `internal/adapters/memindex`（新包）：`memory.Store` 包装器，从 files 后端枚举条目、同步索引、用索引回答 `Search`，未绑定时回退子串搜索。
- `pkg/agentcore/memory`：可选 `Enumerator` 接口与 `Entry.AppliesWhen`；`pkg/agentcore/memory/files`：`Entries` 实现、`applies_when` frontmatter 读写、搜索文本包含 `applies_when`。
- `internal/wiring`：files 后端装配包装器、runtime 迟到绑定、memory 工具 schema/结果渲染、`session_search` kinds 与描述。
- `internal/personal`：`compactTemplate` 固定小节；handoff 条目进 `entries_fts`（常量 + 新迁移回填）。
- 单元测试、`go test` 基线、真实模型一轮（摘要模板）、Linux 容器 `-race` 与 Windows 主机命令。

### 非目标

- 不做向量、embedding、混合检索与 RRF；`zvec-go` 需要随二进制分发原生库，且缺 macOS x64 / Windows arm64，不引入（调研 4.2）。
- 不改 gofer 后端（`gofer memory ls` + 本地子串 AND，`internal/adapters/gofermem/gofermem.go:388-424`）与 exec 协议（`pkg/agentcore/memory/execstore/execstore.go:6-30`）的**检索**行为；只让 exec 透传可选 `applies_when`。
- 不改 `session_search` 的 MATCH/LIKE 分流、bm25 行为、游标与工作区过滤；本次只把 handoff 加入索引类型。
- 不做交接摘要的 JSON 结构解析、不做“任务进展链”、不做 supersede 判断（调研 3.1 明确不借）。
- 不做记忆衰减、`access_count` 回写、自动过期判断；`applies_when` 只给模型阅读，不做版本比较。
- 不新增 `suga memory reindex` 命令、不新增 trace 事件类型、不改 chat 渲染。
- 不改 `MEMORY.md` 的行格式、预算（200 行 / 16KB）与注入顺序。

## 已确认事实与规范

### 记忆检索现状

| 现状 | 代码位置 |
|---|---|
| files 后端的 `Search` 遍历条目目录、逐条 `readEntry`，把 `title + "\n" + body` 小写后做子串 AND，命中按 `Updated` 倒序再按 id，最后截断 | `pkg/agentcore/memory/files/files.go:248-310`（`containsAll` 432-439，`snippet` 444-469，默认 limit 10 在 :50-51） |
| 后端名 `files`，`memory` 工具据此判断结果是否“外部内容”（决定污点） | `pkg/agentcore/memory/files/files.go:43,120-121`；`pkg/agentcore/tools/native/memory.go:28,262-268` |
| 工具层 limit 默认 10、上限 50；结果只渲染 id/kind/title/snippet | `pkg/agentcore/tools/native/memory.go:24-27,246-253,351-365` |
| `Governed.Search` 校验 scope、丢弃非法 id、对 title/snippet 做 `redact.Clean`，不排序不评分 | `pkg/agentcore/memory/governed.go:251-271` |
| `Router.Search` 只按 scope 转发 | `pkg/agentcore/memory/router.go:100-106` |
| gofer 后端用 `gofer memory ls --json` 取列表后在 Go 里子串 AND、按 updated 倒序，`gofer memory search` 未被使用 | `internal/adapters/gofermem/gofermem.go:388-424,428-450` |
| exec 后端每次起进程，`search` op 由外部命令负责 | `pkg/agentcore/memory/execstore/execstore.go:20-21,201-222` |
| 索引预算 200 行 / 16KB，只由 `Write` 经 `Index` 校验；`Index.Bytes` 对 files 就是 `MEMORY.md` 的字节数 | `pkg/agentcore/memory/governed.go:14-17,276-304`；`pkg/agentcore/memory/files/files.go:132-138` |

### session_search 的 FTS5 现状（要复用的部分）

| 现状 | 代码位置 |
|---|---|
| `entries_fts` 是 fts5 虚表，`tokenize='trigram'`，用 `session_id/seq` 定位条目；会话删除触发器清理 | `internal/personal/store/migrations/20261010-234359-add-ledger-fts-memory-meta.sql:12-26` |
| 索引类型常量、role/text 表达式与“改了必须写新迁移”的约定 | `internal/personal/store/fts.go:14-63`（`ftsIndexedTypes` :32，`ftsTextExpr` :44-50，`ftsInsertEntrySQL` :62-63） |
| 新条目入索引在写入事务内完成 | `internal/personal/store/entries.go:15,53` |
| 查询：≥3 rune 的词进 MATCH（短语转义）并按 `bm25()` 排序、`snippet(...,64)` 出片段；1–2 rune 的词走同一条 SQL 的 LIKE；两者可同时使用 | `internal/personal/store/fts.go:141-258`（分流 158-190，排序 177，snippet 174） |
| 这些辅助全部未导出：`ftsMatchExpr`、`escapeLike`、`likeSnippet`、`indexFold`、`minMatchRunes`、`joinWhere` | `internal/personal/store/fts.go:67-72,311-370` |
| 包外可用的只有 `SearchEntries/SearchEntriesPage/MemoryMetaRows` 与 `DB()` | `internal/personal/store/fts.go:133`、`fts_snapshot.go:27`、`store.go:117` |
| 迁移按文件名字典序应用，`//go:embed migrations/*.sql`，无需注册表 | `internal/personal/store/store.go:21-22,124-181` |
| 漂移测试把常量与迁移 SQL 逐字比对（按文件名后缀 `-add-ledger-fts-memory-meta.sql` 取迁移） | `internal/personal/store/fts_test.go:355-397`（:360-364 取文件，:370-371 断言） |

### 分层与装配现状

| 现状 | 代码位置 |
|---|---|
| `pkg/agentcore` 不得 import `internal/*`；`memory` 只允许 `redact`，`memory/files` 只允许 `memory` | `pkg/agentcore/layering_test.go:88-123`（表 :50-52,61） |
| `modernc.org/sqlite` 只被 `internal/personal/store` 空白导入，`pkg/agentcore` 内没有 sqlite 依赖 | `go.mod:17`；`internal/personal/store/store.go:18,97` |
| memory 后端在 `buildMemory` 里选型并注册到 Router（files 用户态 `DirLayout`，项目态 `RepoLayout`/`DirLayout`） | `internal/wiring/memory.go:158-175,177-200,202-277` |
| `Governed` 与 memory 工具在 `newMemoryRuntime` 组装；工具拿到的 `Backend` 回调即 `memory.BackendName(Route(s))` | `internal/wiring/memory_runtime.go:143-170`（:160-166） |
| 会话库在 runtime 里 `store.Open(dbPath)` 后**迟到绑定**：`memoryHost` 先建，`memHost.bind(runner, repo)` 在组装末尾；`Policy.Provenance` 走同一模式 | `internal/wiring/runtime.go:244,487-489,551,648`；`internal/wiring/memory_runtime.go:77-118,151` |
| `suga manifest` 用未绑定的 `&memoryHost{}`（只描述配置，不做检索） | `internal/wiring/manifest.go:400` |

### 交接摘要现状

| 现状 | 代码位置 |
|---|---|
| 摘要请求的 system prompt 与固定小节模板；模板只在摘要请求里出现 | `internal/personal/compact.go:28-46`（`compactTemplate` :37-46），拼进请求 :446-469（:449） |
| 分段交接：`buildPackage` 产出 body + 最近 3 条用户请求 + 插话 + 计划原文，渲染成 `Summary` 存进 `compaction` 条目的 `$.summary` | `internal/personal/segment_handoff.go:18-36,63-83,274-286`；`internal/personal/compact.go:363-378`；记录结构 `internal/personal/context.go:82-96` |
| `--from` 交接写 `handoff` 条目，`$.summary` 复用源会话的摘要或重新生成整包 | `internal/personal/handoff.go:18-25,94-125,139-152` |
| handoff 条目只当“第 0 段交接包”读取，不作为独立 user 消息回放 | `internal/personal/context.go:128-130,163-171,186` |
| handoff **不在** FTS：`ftsIndexedTypes` 只有 user/assistant/tool/compaction，且 ELSE 分支读 `$.Content`，handoff 记录只有 `$.summary` | `internal/personal/store/fts.go:27-51` |
| `session_search` 的 kinds 白名单（存储层与规范化各一份），工具 schema 是逗号分隔字符串、无 enum | `internal/personal/store/fts.go:300-309`；`fts_cursor.go:210-229`；`pkg/agentcore/tools/native/ledger.go:101-102,126,241-258` |
| 段与缓存规则：稳定 system prompt 不随段变化；交接包只在段开始时重建（本设计不改） | `docs/design/2026-10-11-segments-projection-design.md:51,108-133,187` |
| 设计已把“空闲时提炼记忆”和“语义检索”列为非目标 | `docs/design/2026-10-11-segments-projection-design.md:56`；`docs/design/2026-10-16-session-search-cjk-design.md:69-74`；`docs/design/2026-10-10-state-memory-extensibility-design.md:265` |

### 记忆条目格式与工具契约

| 现状 | 代码位置 |
|---|---|
| `Entry{ID,Title,Body,Kind,Updated,Prov}`、`Op{Action,ID,Title,Body,Kind,Prov}`、`IndexLine{ID,Title,Kind}` | `pkg/agentcore/memory/types.go:121-153,191-201` |
| 条目文件是扁平 frontmatter + 正文，手写解析器只认 kind/title/updated/source/session | `pkg/agentcore/memory/files/index.go:154-187,189-228` |
| 工具 schema 属性：action/scope/kind/title/body/id/query/limit | `pkg/agentcore/tools/native/memory.go:90-115` |
| `memory get` 渲染 `# title` + meta 行 + 正文 | `pkg/agentcore/tools/native/memory.go:343-349` |
| `memory` 工具描述仍写“system prompt 里的索引在会话开始时冻结”（已过时，段上下文按段冻结） | `pkg/agentcore/tools/native/memory.go:68-82`（:69）；调研 1.3 |
| exec 协议的 entry/op 形状 | `pkg/agentcore/memory/execstore/execstore.go:20-30,86-123,224-238` |

## 总体方案

三件事共用一条原则：**Markdown 文件与会话底账仍是唯一真源，新表/新字段都是可重建的派生数据或可选注释。**

```text
memory search（files 后端）
  memory.Governed -> Router -> memindex.Store（内存包装器）
       ├─ files.Store.Entries（枚举条目：标题/正文/kind/updated/applies_when）
       ├─ store.SyncMemoryIndex（按 content_hash 增量 upsert/delete，单事务）
       └─ store.SearchMemoryIndex（trigram MATCH + bm25；1–2 rune 退化 LIKE）
             未绑定会话库/未实现枚举 -> 直接委托 files.Store.Search（子串 AND，现状行为）

交接摘要
  compactTemplate（固定小节）-> 摘要请求 -> compaction/handoff 条目的 $.summary
       └─ entries_fts 新增 'handoff' 类型 + $.summary 文本表达式（新迁移回填）
             session_search(kinds=handoff) 可搜

记忆条目
  frontmatter 增加可选 applies_when（可多行）
       ├─ memory get 展示；索引行不展示（不占预算）
       ├─ files.Search 与 memory_fts 的文本都包含它（可命中）
       └─ exec 协议透传（可选字段，版本仍为 1）
```

### 为什么需要这些机制（与更简单做法的比较）

| 机制 | 更简单的做法 | 为什么选当前做法 |
|---|---|---|
| `memory_fts` 派生索引 | 在 Go 里对 ≤200 条算词频/BM25 类似分 | 两套词面实现要各自证明短词、CJK、snippet 行为；trigram + 3 rune 门限 + bm25 + snippet 已在 `entries_fts` 实测并有测试覆盖，复用一份比分叉两份便宜 |
| 每次 `Search` 前增量同步 | 写入时维护 + 手改时靠 fsnotify/定时重建 | 记忆条目 ≤200 个、多为几 KB，读全部文件并比对 sha256 是毫秒级；写入路径不新增失败点，手改天然被发现，无守护进程、无跨进程协调 |
| 换一张 `memory_handoff_fts` 新表 | 新建独立表 + 独立查询 API | handoff 已经是同一底账里的条目，复用 `entries_fts` 只需常量+分支+回填迁移；`session_search` 直接获得能力，不新增调用面 |
| 交接摘要固定小节 | 解析成 JSON 字段（coords/adopted/...） | 消费方是模型与 FTS，不是程序；固定小节已可被 grep/肉眼定位，解析器会带来手改即坏、旧记录不兼容两种失败 |
| frontmatter 多行 `applies_when` | 正文里写一段“适用条件” | frontmatter 可以被 `memory get` 结构化展示、被搜索索引，且不占 `MEMORY.md` 预算；正文里写则完全依赖模型的措辞 |

## 架构

| 组件 | 改动与 owner |
|---|---|
| `internal/personal/store/migrations/20261017-*-add-memory-fts.sql` | 新表 `memory_fts`（fts5 trigram，单文本列 + `scope`/`entry_id`/`content_hash`/`updated` UNINDEXED）与 DOWN 段；不触碰 `entries_fts`。 |
| `internal/personal/store/memfts.go`（建议新文件） | `MemorySource`、`MemoryHit`、`(*Store) SyncMemoryIndex`、`(*Store) SearchMemoryIndex`；直接调用同包未导出的 `ftsMatchExpr`/`escapeLike`/`likeSnippet`/`minMatchRunes`，不复制实现。 |
| `internal/adapters/memindex`（新包） | `memory.Store` 包装器：`Bind(*store.Store)`、`Search` 同步+查询+补齐 title/kind、未绑定回退；`Index/Get/Write` 直接委托。唯一知道“派生索引”的地方。 |
| `pkg/agentcore/memory` | 可选接口 `Enumerator`（`Entries(ctx, Scope) ([]Entry, error)`）与 `Entry.AppliesWhen []string`、`Op.AppliesWhen`；`Store` 四方法签名不变。 |
| `pkg/agentcore/memory/files` | 实现 `Entries`（复用现有目录扫描/读取）；`renderEntry/parseEntry` 增 `applies_when`；`Search` 文本加入 `applies_when`（回退路径与索引路径一致）。 |
| `internal/wiring/memory.go` / `memory_runtime.go` / `runtime.go` | files 后端在 `buildMemory` 里包一层 `memindex.Store` 并放进 `MemorySetup`；在 `memHost.bind` 同一处 `Bind` 会话库；工具 `Backend` 回调不变（仍是 `files`）。 |
| `pkg/agentcore/tools/native/memory.go` | schema 增 `applies_when`；`get` 渲染它；描述修正过时的“会话开始时冻结”文案。 |
| `internal/personal/compact.go` / `segment_handoff.go` | `compactTemplate` 改固定小节；`refsHeading` 与拼接逻辑不变。 |
| `internal/personal/store/fts.go` / `fts_cursor.go` / 新迁移 | `ftsIndexedTypes` 加 `'handoff'`、`ftsTextExpr` 加 handoff 分支、kinds 白名单加 `EntryHandoff`。 |
| `pkg/agentcore/tools/native/ledger.go` | kinds 描述与允许集合加 handoff。 |

## 关键流程

### memory search（files 后端，已绑定会话库）

1. 工具解析 query/limit（10、上限 50），`Governed` 校验 scope 并转发到 `memindex.Store`。
2. `memindex` 用 `Enumerator.Entries` 枚举条目目录里所有合法 id 的条目（与 `files.Search` 同一枚举规则，跳过不可读/越界符号链接）。
3. 构造文本 `title + "\n" + body`，有 `applies_when` 时追加 `"\napplies_when: " + strings.Join(...)`；计算每条的 sha256。
4. `SyncMemoryIndex(scopeKey, sources)`：单事务内读出该 scope 的 `(entry_id, content_hash)`，新增/变更的行先 DELETE 再 INSERT，缺失的行 DELETE；`scopeKey` 用 `memory.Scope.String()`。
5. `SearchMemoryIndex(scopeKey, q, limit)`：按空白切词；≥3 rune 的词组成 MATCH 短语、`ORDER BY bm25(memory_fts), updated DESC, entry_id`；存在 1–2 rune 的词时并上 `text LIKE ? ESCAPE '\'`；若没有任何 ≥3 rune 的词，则整条走 LIKE 并按 `updated DESC` 排序（与 `entries_fts` 的分流一致）。
6. 命中 id 用第 2 步的枚举结果补齐 title/kind；枚举里已没有的 id 被丢弃（并发删除）。返回 `[]memory.Hit`。

### 降级与失败

- 会话库未绑定（`suga manifest` 等只读路径）、`SyncMemoryIndex` 或查询报错时，`memindex.Store` **委托底层 files 后端的子串搜索**并返回其结果；错误只记 `slog.Debug`，不把搜索变成错误。
- 非 files 后端（gofer/exec）不包装，`Search` 行为完全不变。

### 交接摘要

1. 段开始/`--from` 时 `summarize` 用新的 `compactTemplate`；模型只写固定小节。
2. `renderPackage` 照旧在正文后追加最近请求、插话、计划原文；`refsHeading` 字符串不变，`parseRefs` 与 `markdownSection` 无需改动。
3. 摘要写入 `compaction`/`handoff` 条目的 `$.summary`；`AppendEntries` 用新的 `ftsIndexedTypes`/`ftsTextExpr` 入索引（handoff 分支读 `$.summary`）。
4. 跨会话：`session_search(query, kinds=handoff)` 返回 handoff 摘要条目，`recall` 读原文窗口。

### applies_when 写入与读取

1. `memory` 工具 `add/replace` 接受可选 `applies_when`（字符串数组）；`replace` 是整条重写，未给该字段即清空。
2. `files.renderEntry` 每条渲染一行 `applies_when: "<value>"`（Go 字符串引号，与 title 一致），`parseEntry` 遇重复键追加成切片。
3. `memory get` 在 meta 行追加 `applies_when: a · b`；索引行（`IndexLine.Render`）不变，因此不占 200 行 / 16KB 预算。
4. 搜索命中：索引文本与回退路径的 `files.Search` 文本都包含它。

## 安全、数据、运维与回滚

- **污点与脱敏不变**：索引只从 files 后端的条目文本派生，`Governed.Search` 仍对 title/snippet 过 `redact.Clean`；`memory_fts` 不写入任何会话内容或外部内容，因此不扩大污点面。
- **幂等与一致性**：索引是派生数据，任何损坏都可以 `DELETE FROM memory_fts` 后由下一次搜索重建；版本升级靠迁移（新迁移可整表清空并回填）。
- **并发**：同进程内 `SyncMemoryIndex` 由 `memindex` 的互斥锁串行；跨进程沿用现有 SQLite 锁与“最后写入胜”的语义（与 `memory_meta`、`entries_fts` 一致）。两进程交替搜索时最坏情况是重复同步一次，不会返回错误数据（结果以文件为准补齐）。
- **Windows**：表与查询不涉及路径比较，无大小写折叠问题；trigram 分词对非 ASCII 大小写不折叠，与 `session_search` 的既有局限一致（`docs/design/2026-10-16-session-search-cjk-design.md:362`）。
- **数据迁移**：新增两个迁移（`memory_fts` 建表、handoff 回填）。旧的 `compaction` 摘要仍可搜；旧 `handoff` 条目在新迁移里一次性回填。旧版本二进制打开新库时会忽略 `memory_fts`（未知表不影响读写），`entries_fts` 多出的 handoff 行对旧版本的查询不可见（kinds 白名单不含它，但 `MATCH` 未带 kinds 时旧版本会返回 role=handoff 的命中——需在计划里接受这一良性差异）。
- **回滚**：删掉 `memindex` 包装（Router 恢复直接持有 files 后端）即回到子串搜索，`memory_fts` 表残留无害；交接模板改回旧字符串即可，已写记录仍可搜（FTS 只认 `$.summary` 文本）；`applies_when` 是可选字段，旧二进制读新条目会忽略该行（`parseEntry` 未知键跳过），新二进制读旧条目得到空切片。
- **预算**：`MEMORY.md` 行格式与字节数不变；`applies_when` 只进条目文件，不影响 `Index.Bytes`。

## 决策

| ID | 决策 | 理由 |
|---|---|---|
| D1 | 索引放会话底账同一个 SQLite（`memory_fts`），files 的 Markdown 仍是唯一真源 | 不需要第二个存储文件或额外生命周期；派生数据随时可重建 |
| D2 | 每次 `Search` 前按 content_hash 增量同步；迁移时整表重建 | 条目 ≤200、读取成本毫秒级；手改天然被发现；写入路径不新增失败点 |
| D3 | 查询实现放进 `internal/personal/store`，复用未导出的 trigram 分流辅助 | 导出/复制辅助会引入第二套需要单独证明的行为；同包直接复用 |
| D4 | 只有 files 后端走索引；gofer/exec 保持各自检索 | 各自已有检索能力，改造收益小、风险大；保持 `memory.Store` 契约 |
| D5 | 用可选接口 `Enumerator` 暴露“枚举条目”，不改 `Store` 四方法 | 包装器需要批量读，其他后端不必实现；不影响既有后端 |
| D6 | 包装器迟到绑定会话库，未绑定或出错时回退子串搜索 | 复用 `memoryHost.bind` 的既有模式；搜索是只读能力，降级比报错更有用 |
| D7 | 交接“结构化”= 固定小节文本，不解析为 JSON 字段 | 消费方是模型与 FTS；解析器会带来手改即坏与旧记录不兼容 |
| D8 | handoff 复用 `entries_fts`，加类型 + `$.summary` 文本分支 + 新迁移回填 | 同一条底账里已有记录；`session_search` 免费获得能力，无新查询面 |
| D9 | `applies_when` 用 frontmatter 重复键（数组），只进条目文件与搜索文本 | 保持扁平手写解析器；不占索引预算；向后兼容 |
| D10 | 搜索失败一律降级为子串搜索，不做 fail-closed | 记忆检索是只读增强，返回旧行为优于让模型看不到记忆 |
| D11 | 不新增 trace 事件、不改 chat 渲染 | 三件事的结果都能通过现有工具结果与 `session_search` 观测 |

## 待确认事项

| ID | 问题 | 建议结论 |
|---|---|---|
| Q1 | `memory_fts` 是否也要索引 `MEMORY.md` 里的手写行（非条目）？ | 不索引：手写行不是条目，`memory get` 也读不到；`MEMORY.md` 由文件工具阅读 |
| Q2 | 迁移回填 handoff 后，旧版本二进制做一次不带 kinds 的搜索会看到 role=handoff 命中，是否可接受？ | 可接受：suga 与自身库版本同步，回滚时最坏多几条良性命中 |
| Q3 | `applies_when` 是否需要数量/长度上限？ | 需要：单行、≤200 rune、最多 10 条，超出报 `ErrInvalid`，避免 frontmatter 失控 |
| Q4 | 交接模板删掉“## 当前计划”小节是否影响现有消费方？ | 不影响：计划原文由 `renderPackage` 追加（`segment_handoff.go:282-284`），唯一按小节解析的是 `refsHeading`（不变） |
| Q5 | 是否给 `memory search` 结果加分数显示？ | 不加：排序已表达相关度，分数量纲（bm25 负值）对模型无意义 |

## 结论与人工计划 Gate

本设计为 `suga-xis4`、`suga-fuw0`、`suga-8nzb` 的合并 Draft 0.1，待用户审批。批准后按 `docs/plans/2026-10-17-memory-search-handoff-plan.md` 分三组独立任务实施；审批前不改 `memory_fts`、不改 `compactTemplate`、不加 `applies_when` 字段。
