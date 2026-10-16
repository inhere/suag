<!-- template_id: design; template_version: 1.1.1 -->
# 中文可用的 session_search（混合匹配、cursor 分页、kind/时间过滤与 cwd 下推）设计

> 状态：Draft 0.3（按确认评审 r2 修订，待用户审阅）

## 修订记录

| 版本 | 日期 | 作者 | 摘要 |
|---|---|---|---|
| 0.1 | 2026-10-16 | suga（gofer job） | 初稿（含 FTS5 实测） |
| 0.2 | 2026-10-16 | suga（gofer job） | 按评审 r1 关闭 S-C1/S-M1–S-M6；S2 改为后续独立设计前提，不纳入本期 |
| 0.3 | 2026-10-16 | suga（gofer job） | 按确认评审 r2 将正文统一为进程内快照游标，关闭 S2 旧表述、500 条上限/TTL、指纹与自身结果污染等一致性项 |

> 仅语义变化递增版本；纯 identity/provenance/元数据纠正沿用原版本，并在 Git/进度记录中留痕。

来源：issue suga-3h29；调研 `docs/research/2026-10-16-feature-strengthening-analysis.md` §3 #4（中文短词整条退化为全表扫描）、§3 #5（无分页、workspace scope 在 Go 里拼大 IN），及 §5「FTS5 trigram 的中文行为依赖 modernc.org/sqlite 的实现，本文按代码里的短词退化分支判断，未用真实库实测，改检索前应先验证」——本设计先做了该实测。独立评审：`local/reviews/2026-10-16-budget-search-design-review-r1.md`（不入库）。参考同类产品：SQLite FTS5 官方 trigram/unicode61 文档、Claude Code / Codex 的会话检索（工具式"搜索 + 按 ref 取回"的两段式，**参考其交互形态，不照搬其索引实现**）。

### 思考模式与范围冻结

- thinking_mode：RIGOROUS。
- core_objective：中文（尤其 1–2 字词）查询不再"整条退化为 LIKE"；命中可翻页、可按条目类型与时间过滤；`workspace` scope 的目录条件下推到 SQL 而不是把全部会话 cwd 读进 Go 拼大 `IN`。
- allowed_scope：`internal/personal/store`（`fts.go`、迁移、`entries.go`、`sessions.go`）、`internal/personal`（`ledger.go` 的 scope 解析）、`pkg/agentcore/tools/native/ledger.go`（工具 schema、描述、结果渲染）、`internal/cli/sessions.go`（可选：`--kind`/分页）、测试与基准（本设计首次引入 `func Benchmark`）。
- non_goals：长期记忆检索（`pkg/agentcore/memory/files`，§3 #6）；向量/语义检索；跨 `SUGA_HOME` 检索；`trace`/`stats` 的分页与 kind 过滤（§3 #29 的另一半）；`redact` 规则本身的改动；检索质量的自动评测体系（另案）。
- expansion_policy：DEFER_OR_REQUEST。
- review budget：低暴露度（个人工具、检索为只读、可一键退回旧路径；SR1409）。
- 停止条件：确认无 CORE_BLOCKING 后请用户批准，进入计划阶段。

## 背景与目标

现状（详见「已确认事实与规范」）：会话全文索引是 FTS5 `tokenize='trigram'`（`entries_fts`），而 trigram 对少于 3 个 rune 的词无能为力；代码的处理是"任一词不足 3 rune → 整条查询改走 `LIKE '%词%'` 全表扫描、按时间倒序、没有 bm25"（`internal/personal/store/fts.go:99-141`）。中文里 2 字词是常态（"缓存"、"迁移"、"预算"），于是一句正常的中文查询就丢掉了索引与排序质量；`LIMIT` 硬编码、没有游标分页；`workspace` scope 还要把全部会话的 `id → cwd` 读进 Go、逐个用 `pathWithin` 判断再拼 `IN (...)`（`internal/personal/ledger.go:144-167`）。

本设计的实测（§已确认事实 B）改变了两个前提：

1. **不是"MATCH 报错才退化"**：短短语在 trigram 索引上 `MATCH` **不报错、返回 0 行**（单短语）或在复合查询里**被静默丢弃**（`"中文" "token"` 返回只含 `token` 的行）——后者是一个"悄悄放宽条件"的正确性隐患，现状用整条退化绕开了它。
2. **`tokenize='unicode61'` 对中文不可用**（连续 CJK 被当成一个 token，"中文"只在被标点分隔时才成为 token），因此"换 tokenizer"这条路直接排除；而**应用层切 2-rune 滑窗（bigram）后用 unicode61 索引**，对 2 字与更长的中文词召回与 `LIKE` **完全一致**，并且给得出 bm25——但索引体积约为原文的 3.9 倍（实测），且回填需要 Go 侧参与。

用户可见目标：

1. 中文 1 字 / 2 字 / 3 字以上，以及中英混合的查询都能给出正确召回；长词命中仍有 bm25 相关度排序。
2. 混合查询（既有 2 字中文又有英文词）**不再整条退化**：长词继续走索引，短词作为过滤条件。
3. 命中多时能翻页（`next_cursor`），不靠"把 limit 开大"。
4. 能只搜某种条目（`kind`）或某个时间窗（`since`/`until`）。
5. `workspace` scope 在大库上不再先读全部会话 cwd 再拼 `IN`。
6. 不改坏现有行为：`session_search` / `recall` 的既有调用（含 `ref=out:` 的行扫描、`scope` 语义、`redact`）逐字节兼容。

## 名词

- **term（查询词）**：`session_search` 的 `query` 按空白切分后的一个词；现状语义是"每个词都必须出现"（AND）且**按字面匹配**（不是 FTS5 语法）。
- **rune / 字符长度**：词里 Unicode 码点的个数。trigram 的可用下限是 3（`internal/personal/store/fts.go:69-70` `minMatchRunes = 3`）。
- **trigram 索引**：FTS5 `tokenize='trigram'`，把文本切成连续 3 rune 的片；支持 ≥3 rune 的**子串**匹配。
- **bigram 方向**：后续可评估的 CJK 辅助索引；不属于本 Draft 的实现方案。
- **MATCH / LIKE 路径**：现状两种检索实现（索引匹配 / 子串全表扫描）；本设计按词分流，两者可共存于一条 SQL。
- **cursor（游标）**：进程内稳定快照的位置，编码 `snapshot_id + offset`，不把 bm25/时间作为可漂移排序键。
- **scope**：`current`（当前会话）/ `workspace`（当前目录及其子树）/ `all`。
- **kind（条目类型）**：`entries_fts.role`，取值 `user` / `assistant` / `tool` / `compaction`（`internal/personal/store/fts.go:31` 只索引这四类）。
- **ref**：命中的定位串 `<session_id>/<seq>`（消息）或 `out:<session_id>:<16 hex>`（外置输出）。

## 范围与非目标

范围：

- `store/fts.go`：查询词分流（trigram / LIKE）、进程内快照分页、kind 与时间过滤、cwd 条件与“当前会话”条件的 SQL 下推，以及排除检索工具自身结果。
- 后续方向：记录 bigram 索引的准入前提，但本 Draft 不包含迁移、回填或第二张索引表。
- `personal/ledger.go`：`workspaceSessions` 改为下推（不再读全部 cwd）；`LedgerQuery` 传递新参数。
- `pkg/agentcore/tools/native/ledger.go`：`session_search` 的 schema、描述、结果文本（`next_cursor` 行）。
- 可选：`internal/cli/sessions.go` 的 `--kind` 与 cursor 透出（CLI 现状只显示首个 snippet，`internal/cli/sessions.go:96-110`）。
- 测试与基准：`store/fts_test.go` 扩展、新的等价性测试（下推 vs 旧 Go 实现的逐例一致）、`store/fts_bench_test.go`（仓库首个 benchmark）。

非目标：

- 长期记忆检索、语义检索、跨库检索。
- `trace`/`stats` 的过滤与分页。
- 迁移机制本身（`schema_migrations` 无 DOWN 执行路径，`internal/personal/store/store.go:176-189` 只截取 UP 段；本设计沿用）。
- `redact` 规则；`snippet()` 的高亮样式；`recall` 的 offset 语义（保持不变）。

## 已确认事实与规范

### A. 现状代码（分支基线 `626747f`）

**索引与 schema**

- `entries_fts` 由迁移 `internal/personal/store/migrations/20261010-234359-add-ledger-fts-memory-meta.sql:12-18` 建立：`CREATE VIRTUAL TABLE entries_fts USING fts5(session_id UNINDEXED, seq UNINDEXED, role UNINDEXED, text, tokenize='trigram')`；迁移头部注释明确两点：**它保留自己的 rowid 并按 `session_id/seq` 回连 entry**（`entries` 是隐式 rowid 表，`suga gc` 的 VACUUM 可能重编号），以及索引表达式必须与 `store/fts.go` 的常量一致（由 `TestFTSExpressionsMatchMigration` 守）。
- 删除清理：`:23-26` 触发器 `entries_fts_session_delete AFTER DELETE ON sessions`；回填同迁移 `:30-43`（纯 SQL）。
- 写入：`internal/personal/store/entries.go:15-62` `AppendEntries` 在**同一事务**内插入 entry 后用 `ftsInsertEntrySQL` 插索引行（`internal/personal/store/fts.go:59-62`）。
- 迁移执行：`internal/personal/store/store.go:24-25`（UP/DOWN 标记）、`:118-124`（`schema_migrations`）、`:136-171`（按文件名排序、未应用的在一个事务里执行 DDL 并登记）、`:176-189`（**只截取 UP 段，没有 DOWN 执行路径**）、`:118-120` `migrateUpTo`（测试造旧 schema 用）。
- 表结构：`migrations/20261008-230000-create-core-store.sql:4-13` sessions（`id, title, cwd, status, last_turn_id, created_at, updated_at, meta_json`——cwd 列名就是 `cwd`），`:26-35` entries（`session_id, seq, turn_id, type, json, created_at`，主键 `(session_id, seq)`，索引 `idx_entries_turn(turn_id)`）。会话"kind"不是列而是 `meta_json` 的键：`internal/personal/store/sessions.go:123-133`（`MetaKind`/`KindAgent`/`notAgentSession`）。

**查询实现**

- 限额与阈值：`internal/personal/store/fts.go:64-71`（`defaultSearchLimit = 20`、`maxSearchLimit = 100`、`snippetRadius = 60`、`minMatchRunes = 3`）。
- 查询入参：`fts.go:73-88` `EntrySearch{Query, SessionIDs, CWDPrefix, Limit, IncludeAgents}`；返回 `fts.go:90-97` `EntryHit{SessionID, Seq, Role, Snippet, CreatedAt}`。
- 分流：`fts.go:103-120` `SearchEntries`——`strings.Fields` 切词、`limit` 归一（`:108-112`）、**任一词 `< minMatchRunes` 就把 `useMatch` 置 false**（`:114-120`）。
- MATCH 分支：`fts.go:126-131`（`snippet(entries_fts, 3, '', '', '…', 64)`、`where = entries_fts MATCH ?`、`args = ftsMatchExpr(terms)`、`order = bm25(entries_fts), f.session_id, f.seq`）。
- LIKE 分支：`fts.go:132-141`（每词 `f.text LIKE ? ESCAPE '\'` 用 `AND` 连接、`order = e.created_at DESC, f.session_id, f.seq DESC`）。
- 过滤拼装：`fts.go:142-155`（`SessionIDs` → `IN (placeholders)`；`IncludeAgents=false` → `notAgentSession("ss.meta_json")`；`CWDPrefix` → `substr(ss.cwd, 1, length(?)) = ?`，精确大小写敏感）。
- 查询主体：`fts.go:158-162`（`FROM entries_fts f JOIN entries e ON e.session_id=f.session_id AND e.seq=f.seq JOIN sessions ss ON ss.id=f.session_id WHERE … ORDER BY … LIMIT %d`，`LIMIT` 是 `fmt.Sprintf` 拼进去的常量）。
- 扫描与 snippet：`fts.go:169-186`（MATCH 用 `snippet()` 的输出，LIKE 用 `likeSnippet(text, terms[0])`；`fts.go:206-224` 取首次命中前后 60 rune）。
- 表达式构造：`fts.go:189-198` `ftsMatchExpr`（逐词双引号 + 内部引号双写 → FTS5 语法字符当字面文本）+ `escapeLike`（`fts.go:200-204`）。
- 现有测试：`internal/personal/store/fts_test.go`（397 行）——`TestFTSIndexOnAppend`（`:128`）、`TestFTSSearch`（`:145`，子用例含 `chinese term of 3+ runes uses MATCH` `:160`、`2-rune term falls back to LIKE` `:174`、`FTS syntax characters are literal` `:182`、`multiple terms are AND` `:196`、`session and cwd filters` `:204`、`LIKE snippet keeps 60 runes around the hit` `:213`、`limit defaults to 20 and is capped at 100` `:222`、`empty query is an error` `:234`）、`TestFTSDeleteSessionAndGC`（`:267`）、`TestFTSMigrationBackfillsHistoryOnce`（`:319`）、`TestFTSExpressionsMatchMigration`（`:355`）。测试用 `t.TempDir()` 下的**真实文件库**（`internal/personal/store/store_test.go:13-19` `openTemp`），不用 `:memory:`（全仓无匹配）。
- 无既有 cursor/分页惯例：`store/` 下只有 `internal/personal/store/types.go:143-148` `EntryFilter{SinceSeq, TurnID, Types, Limit}`；无 `func Benchmark`（全仓零匹配）。

**上层与工具**

- `internal/personal/ledger.go:93-135` `Ledger.Search`：`scope=all` 不加限制；`current`/`workspace`/`""` 需要当前会话（`observeScopeFrom(ctx).sessionID`），非 current 时调 `workspaceSessions`；`:121-123` 空列表直接返回空（**绝不把空列表当"全部"**）。
- `internal/personal/ledger.go:144-167` `workspaceSessions`（`ledger.go:111` 是它唯一调用点）：`repo.GetSession` 取当前会话 `Cwd`，空则只返回当前会话；否则 `repo.SessionCwds(ctx)` 取全部 `id → cwd`，`pathWithin(sess.Cwd, cwd, l.foldCase)` 判断（`foldCase = runtime.GOOS == "windows"`，`ledger.go:73`）。
- `internal/personal/ledger.go:169-184` `pathWithin`：`filepath.Clean` + 可选小写化，**按分隔符边界**比较（`/repo` 不匹配 `/repo2`）。
- `internal/personal/store/sessions.go:102-117` `SessionCwds`：`SELECT id, cwd FROM sessions WHERE cwd <> '' AND notAgentSession("meta_json")`——注释说明"只取两列、排除子代理会话，供调用方自行按路径边界匹配"。
- `internal/personal/ledger.go:26-42` 窗口上限常量（`maxRecallMessages = 100`、`maxRecallLines = 2000`、`maxLineHits = 100`、`maxRecallOffset = 100000`）；`:255-290` `readMessages`（offset/limit 分页，`SinceSeq: seq-1`）、`:399-438` `readOutputLines`（1-based 行号窗口）、`:442-479` `searchOutput`（`ref=out:` 时逐行大小写不敏感子串匹配）。
- 工具：`pkg/agentcore/tools/native/ledger.go:78-108`（`session_search` 的 name/description/schema：`query`/`scope`/`ref`/`limit`，required `query`）、`:154-176` `renderLedgerHits`（标题 `%d hit(s) in scope %s; read one with recall(ref):`，正文 `<ref>  <role>  <snippet>`，snippet 用 `strings.Fields` 压成单行）、`:122` **整个结果过 `redact.Clean`**；`recall` 在 `:178-252`（描述与 schema 见 `:192-221`）。
- 注册与分类：`internal/wiring/runtime.go:324-325`、`internal/wiring/manifest.go:262-263`；ACP 映射 `internal/acpserver/policy.go:368`（`CategorySearch → ToolKindSearch`），测试 `internal/acpserver/policy_table_test.go:81-85`。
- CLI：`internal/cli/sessions.go:32-49`（`--query/-q`、`--cwd`、`--since/--until`、`--agents`、`--json`、`--limit` 默认 10）、`:102`（`store.EntrySearch{Query: opts.query, Limit: 100, IncludeAgents: opts.agents}`）、`:106-110、175-177`（去重后显示首个 snippet）；**没有任何检索相关配置键**（`session_search` 的默认 limit 与短词阈值是硬编码的，`fts.go:64-71`）。
- 端到端测试参考：`internal/personal/ledger_test.go:357-389`（断言 `"1 hit(s) in scope workspace"` 与 `"s_old/1  user  "`）。

### B. 实测（本次新增，命令与输出摘要）

环境：worktree `.worktrees/b8d2`，`go 1.26.0`，`modernc.org/sqlite v1.60.1`（`go.mod:17`），`sqlite_version() = 3.53.4`（与仓库同版本的驱动，真实 SQLite 实现，不是 mock）。

程序（**临时，位于 `tmp/`（已 gitignore），未提交**）：

```text
go run ./tmp/ftsprobe    # 检索行为矩阵、语法/转义、2 万与 10 万行规模基准、体积与建索引耗时、查询计划
go run ./tmp/ftsprobe2   # 短短语在复合查询里的行为与 FTS/LIKE 组合实验
go run ./tmp/ftsprobe3   # MATCH+LIKE 同句并存；两个 FTS 表同时 MATCH 的 JOIN/子查询/IN 形态
```

实测输出摘要（原文见程序输出；下列为关键行）：

1）**trigram 对短词：不报错、返回 0 或被丢弃**

```text
== query "中"      MATCH fts_tri(trigram) 0 hits      LIKE docs.text 5 hits
== query "中文"     MATCH fts_tri(trigram) 0 hits      LIKE docs.text 5 hits
== query "中文检"   MATCH fts_tri(trigram) 2 hits #8(-2.16) #1(-1.13)   LIKE 2 hits [1 8]
== query "TOKEN"   MATCH fts_tri(trigram) 2 hits（与 "token" 相同 → 大小写不敏感）
-- 复合查询（ftsprobe2）
  "中文"             hits=[]            # 单短语、无 trigram → 空
  "中文" "token"     hits=[3]           # 短短语的约束被静默丢弃，只剩 token！
  "中文" AND "token" hits=[]            # 显式 AND 下短短语导致整体空
  "不存在的词" "token" hits=[]            # 长短语无命中 → 空（正常）
```

2）**`tokenize='unicode61'` 在中文上不可用**

```text
== query "中文"   MATCH fts_uni(unicode61) 2 hits  #7 #9     # 只有被标点分隔/独立成词的两处命中
== query "检索"   MATCH fts_uni(unicode61) 0 hits            # 语料里"检索"出现在连续 CJK 串中 → 搜不到
```

3）**bigram 辅助列（unicode61）召回与 LIKE 一致**

```text
== query "中文"    BIGRAM bigram+unicode61 5 hits   LIKE docs.text 5 hits
== query "检索"    BIGRAM 2 hits                     LIKE 2 hits
== query "分页"    BIGRAM 2 hits                     LIKE 2 hits
== query "预算"    BIGRAM 2 hits                     LIKE 2 hits
== query "迁移"    BIGRAM 1 hit                      LIKE 1 hit
== query "数据库"   BIGRAM 1 hit                      LIKE 1 hit
== query "中"      BIGRAM 0 hits（切不出 bigram）      LIKE 5 hits（1 字只能 LIKE）
```

4）**规模基准**（合成中文语料，10 次平均；第 2 万行 / 第 10 万行）

```text
n=100000（约 9.6 MB 文本）
  trigram MATCH "中文"            0 hits（不可用）      bigram 6.66ms 100000 hits   LIKE 47.1ms 100000 hits
  trigram MATCH "中文检索"       23.9ms 100000 hits    bigram 18.8ms 100000 hits   LIKE 51.9ms 100000 hits
  trigram MATCH "迁移"            0 hits               bigram 6.76ms 100000 hits   LIKE 74.3ms 100000 hits
  trigram MATCH "sum_session"/"ZEBRA"（选择性：10 命中）
  trigram MATCH "ZEBRA"          0.15ms  10 hits      bigram 0.25ms 10 hits       LIKE 60.9ms 10 hits
  trigram MATCH "第000005条"      0.47ms   1 hit       bigram 3.20ms 315 hits      LIKE 81.9ms 1 hit
-- 查询计划
  select * from fts_tri where fts_tri match '中文检索'      -> SCAN fts_tri VIRTUAL TABLE INDEX 0:M1
  select * from fts_bigram where fts_bigram match '"中文"'  -> SCAN fts_bigram VIRTUAL TABLE INDEX 0:M1
  select * from docs where text like '%中文%'               -> SCAN docs
-- 体积与建索引耗时（相对"纯表"）
  plain:   22,822,912 B 1.00×  写入 0.78s
  trigram: 77,041,664 B 3.38×  写入 4.44s
  bigram:  88,793,088 B 3.89×  写入 4.87s
  （n=20000 时：3.51× / 3.94×，写入 0.29s / 0.34s）
```

结论（用于本设计）：选择性查询索引优势极大（0.15ms vs 60.9ms，约 400×）；**当命中集合接近全集时优势消失**（命中本身就是代价），这正是"缩短词 → 命中多 → 慢"的真实成因；索引体积约 3.4–3.9× 原文，新增 bigram 索引等于把索引体积再加一倍。

5）**MATCH 语法必须逐词加引号**（现状 `ftsMatchExpr` 已做，改造后必须保持）

```text
  中文"        ERR SQL logic error: unterminated string
  -中文        ERR no such column: 中文
  (中文         ERR fts5: syntax error near ""
  a-b          ERR no such column: b
  budget.tokens ERR fts5: syntax error near "."
  *            ERR unknown special query
  cost_usd     ok  hits=1
  （全角标点 ，！ 不是 FTS5 语法字符，未加引号也不报错）
```

6）**分页与组合的可行性（ftsprobe2/3）**

```text
-- bm25 与 MATCH/LIKE 组合实验结果（本 Draft 不采用分数游标）
  select count(*) from fts where fts match '中文检索' and bm25(fts) < -0.5  -> ok
  select rowid, score from (select rowid, bm25(fts) as score from fts where fts match '中文检索')
    where (score, rowid) > (-1.0, 3) order by score, rowid limit 5          -> ok [4(-0.48) 1(-0.23)]
  select rowid from docs where (id, text) < (4, 'z') order by id desc limit 2 -> ok [3 2]
-- MATCH 与 LIKE 同一条 SQL 并存，且仍用 FTS 索引（计划 0:M1）
  ... from fts_tri where fts_tri match '"token"' and text like '%中文%' escape '\'  -> 0 hits（语义正确）
  ... from fts_tri where fts_tri match '"session_search"' and text like '%中文%'      -> 2 hits
-- FTS 表上的 LIKE 会尝试 trigram 的 LIKE 优化（3+ 连续字符走索引；2 字符无索引可用）
  select * from fts where text like '%中文检索%'  -> SCAN fts VIRTUAL TABLE INDEX 0:L0
  select * from fts where text like '%中文%'      -> SCAN fts VIRTUAL TABLE INDEX 0:L0（无 trigram 可用，退化为扫描该列）
-- 两个 FTS 表同时 MATCH 的三种形态都可用（ftsprobe3）
  from fts_tri f join fts_bi b on b.id=f.id where f.fts_tri match '"中文检索"' and b.fts_bi match '"中文"'  -> 2 hits
  from fts_tri where fts_tri match '"中文检索"' and (f.id,f.rowid) in (select b.id,b.rowid from fts_bi b where b.fts_bi match '"中文"') -> 2 hits
  from fts_tri where fts_tri match '"中文检索"' and exists (select 1 from fts_bi b where b.id=f.id and b.fts_bi match '"中文"') -> 2 hits
```

（注：ftsprobe3 用 toy 列 `id` 代替真实的 `(session_id, seq)` 键，形态等价；`entries_fts` 的 rowid 会因 `gc` 的 VACUUM 重编号，所以真实实现按 `session_id/seq` 对齐，与迁移注释一致。）

### C. 从实测得到的结论

1. `minMatchRunes = 3` 不是保守估计，而是**硬下限**：trigram 对 1–2 rune 的词一律无命中。

S2 的 bigram 仅作为后续方向记录，不属于本 Draft 的方案、测试、迁移或运维承诺。
2. 现状"整条退化 LIKE"避免了更糟的行为：若把短短语直接塞进 `MATCH`，复合查询里的短词约束会被**静默丢弃**（召回放宽、结果看起来"有结果"但条件没生效）。任何改造都必须保证"进 MATCH 的每个短语都 ≥3 rune"。
3. 换 tokenizer 不可行；本 Draft 不新增辅助索引，2 字中文词走 LIKE。未来若要做 CJK 辅助索引，须另案验证边界、体积与回填一致性。
4. 分页、kind/时间过滤、MATCH+LIKE 并存与 cwd 下推均已在 SQLite 3.53.4 + 本驱动上验证可行；本 Draft 不把双索引实验转为实现承诺。

## 总体方案

本 Draft 只批准 S1：按词分流、进程内快照分页、过滤与 cwd 下推；S2 只记录后续设计必须满足的前提。

**S1（零迁移、不动索引）：按词分流 + 快照分页 + 过滤 + cwd 下推**

1. **按词分流**（修掉"整条退化"）：
   - `runeLen(term) >= 3` → 进 `entries_fts MATCH`（`ftsMatchExpr`，多个词仍用空格隐式 AND，全部 ≥3 rune 才成立）。
   - `runeLen(term) == 2` → 作为 `f.text LIKE '%词%' ESCAPE '\'` 过滤条件；未来 S2 另案评估。
   - `runeLen(term) == 1` → 一律 `f.text LIKE '%词%'`（1 字中文的召回意义有限，工具描述里建议模型用 2 字以上）。
   - 两类条件用 `AND` 组合在同一条 SQL 里（实测形态 A）；**只有当没有任何 ≥3 rune 的词时**才完全没有 `MATCH`（此时退回"纯 LIKE + 时间序"，与现状的纯短词行为一致，但混合查询不再退化）。
   - 排序：有 `MATCH` 时 `ORDER BY bm25(entries_fts), f.session_id, f.seq`（与现状一致，LIKE 词不参与打分但继续作为过滤）；没有 `MATCH` 时 `ORDER BY e.created_at DESC, f.session_id, f.seq DESC`（与现状一致）。
   - `snippet`：有 `MATCH` 用 `snippet(entries_fts, 3, '', '', '…', 64)`；否则用 `likeSnippet(text, terms[0])` 的行为保持不变（多词时仍取第一个词做窗口，现状如此）。
2. **cursor 分页**：`EntrySearch` 增 `Cursor string`，返回 `([]EntryHit, nextCursor string, err error)`（或 `SearchResult` 结构）。
   - 首次查询在当前进程生成完整有序的 `(session_id, seq)` 快照，最多缓存 500 条结果；游标只编码 `v1.<snapshot_id>.<offset>`，后续页按快照偏移取回。快照按查询指纹缓存，TTL 建议 10 分钟：足以完成连续工具调用，又不会长期保留会话结果；内存压力下可提前淘汰。
   - 超过 500 条时不静默截断：返回前 500 条，并在结果中明确“结果超过 500 条，请缩小 query、时间窗、kind 或 scope”；仍可在这 500 条内用 `next_cursor` 翻页。快照缺失、进程重启或 TTL 到期时忽略游标，从头查询并说明。
   - 查询指纹包含解析后的绝对 since/until（按 `timeLayout`）、scope、current session、规范化 workspace、kind 集合与 IncludeAgents；相对时间在首查时固定，不因翻页漂移。
   - 工具层：`session_search` 增可选参数 `cursor`，结果末尾追加一行 `next_cursor: <…>`（仅有下一页时）。
3. **kind / 时间过滤**：`EntrySearch` 增 `Kinds []string`、`Since, Until time.Time`；SQL 用 `f.role IN (…)` 与 `e.created_at >= ? / < ?`（半开区间）。工具参数 `kind`（单值字符串，取值同上）、`since`/`until`（接受 `7d`/`36h`（相对现在）或 `2026-10-01`、RFC3339（绝对））。
4. **cwd / 当前会话条件下推**：`EntrySearch` 增 `WorkspaceDir string`、`CurrentSessionID string`、`FoldCase bool`（或一个 `Workspace *WorkspaceFilter`）；`ledger.workspaceSessions` 与 `SessionCwds` 调用从检索路径移除。SQL 条件：

```sql
AND ( f.session_id = :current
      OR ( ss.cwd <> ''
           AND ( ss.cwd = :dir
                 OR substr(ss.cwd, 1, length(:dirsep)) = :dirsep ) ) )
```

   其中 `dir` 先按 `filepath.Clean` 规范化；根目录不再机械拼接分隔符，SQL 参数由规范化路径与边界谓词生成；`FoldCase`（Windows）时把 `ss.cwd` 与两个参数都包 `lower()`。语义与 `pathWithin` 对齐的点：按分隔符边界（`/repo` 不匹配 `/repo2`）、排除空 cwd（与 `SessionCwds` 的 `cwd <> ''` 一致）、**当前会话始终包含**（即使它没有 cwd）。已知差异（写进文档与测试）：Windows 上 `lower()` 只折叠 ASCII，路径里含大小写不同的**非 ASCII** 字符时不如 Go 的 `strings.ToLower` 精确（现状 `ledger.go:173-175`）；如需严格等价，见决策 D7 的备选（SQL 粗筛 + Go 精判候选）。


**S2（后续独立设计方向，不属于本 Draft 的实施承诺）**

未来若要让 2 字 CJK 词进入索引，必须另起设计并先在真实 `sessions.db` 副本上量化体积、写放大与查询收益。前提包括：迁移事务固化每个 session 的回填边界；用 `BEGIN IMMEDIATE` 认领待回填任务，保证多进程不重复；就绪状态只由待回填任务为空决定；写路径与旧二进制都不能造成静默漏索引，必要时用版本闸门强制退回 LIKE；bigram 只包含 CJK 连续段、`remove_diacritics=0`，并与既有 FTS 行通过稳定 rowid 对齐。S2 不属于本期范围，也不新增配置键。

## 架构

```text
 session_search(query, scope, kind, since/until, limit, cursor)
        │
        ▼
 Ledger.Search (internal/personal/ledger.go)
   ├─ scope: current → CurrentSessionID
   │        workspace → WorkspaceDir + CurrentSessionID（下推到 SQL，不再读全部 cwd）
   │        all → 无限制
   └─ q.Cursor → store.EntrySearch
        │
        ▼
 Store.SearchEntries (internal/personal/store/fts.go)
   ├─ 词分流:  ≥3 rune → MATCH(trigram)
   │          1–2 rune → LIKE 过滤（同一 SQL，AND 组合）
   ├─ 过滤: role IN (kind…) · created_at 区间 · cwd/当前会话下推 · 非子代理会话
   ├─ 排序: bm25(有 trigram 词) / created_at DESC(纯短词)
   └─ cursor: snapshot_id + offset；按快照位置取回 LIMIT n+1 → next_cursor
        │
        ▼
 renderLedgerHits: "<ref>  <role>  <snippet>" … "next_cursor: v1.<snapshot_id>.<offset>"
        │  redact.Clean（游标为纯文本短 token，脱敏不改写）
        ▼
 模型 → recall(ref) 读窗口（offset/limit 语义不变）
```

## 关键流程

### 1. 中文短词与混合查询

```text
S1：query = "中文 token"   → 词 = [中文(2), token(5)]
  SQL: WHERE entries_fts MATCH '"token"' AND f.text LIKE '%中文%' ESCAPE '\'
  计划: SCAN entries_fts VIRTUAL TABLE INDEX 0:M1（用索引；LIKE 作为过滤）
  结果: 只含同时满足两者的行 —— 修掉"短词约束被静默丢弃"（实测 6 里 trigram 会返回只含 token 的行）

S1：query = "缓存"        → 唯一词 2 rune → 无 MATCH：LIKE + created_at DESC（与现状同，但只在真的没有长词时）
未来 S2：若另案批准，再评估 bigram 索引；本 Draft 不执行
query = "检索"（2 rune）→ LIKE；query = "数据库"（3 rune）→ trigram MATCH（现状）
query = "中"（1 rune）→ LIKE
```

### 2. 翻页

```text
第一次: limit=20 → 生成最多 500 条的有序快照，返回 20 条 + next_cursor: v1.<snapshot_id>.20
模型:   session_search(query, cursor="v1.<snapshot_id>.20", limit=20)
SQL:    按快照中的 `(session_id, seq)` 批量取回下一段；不重新计算 bm25 排序
到最后一页: 不再返回 next_cursor 行
超过 500 条: 返回前 500 条并提示结果已截断，请缩小查询；不静默假装结果完整
快照缺失、重启或 TTL 到期: 忽略游标、从头开始，并在结果首行说明"游标已失效，已从头开始"
```

### 3. workspace scope 下推

```text
旧: SELECT id, cwd FROM sessions WHERE cwd <> ''（全部会话）→ Go 逐条 pathWithin → 拼 IN (id1, id2, …)
新: WHERE entries_fts MATCH ? AND ( f.session_id = 's_current' OR ( ss.cwd <> ''
        AND ( ss.cwd = '/repo' OR substr(ss.cwd, 1, length('/repo/')) = '/repo/' ) ) )
    + notAgentSession(ss.meta_json)（未变）
    Windows: lower(ss.cwd) = lower('/repo') OR substr(lower(ss.cwd),1,length(...)) = lower('/repo/')
边界用例（等价性测试固定）: /repo vs /repo2 vs /repo/sub vs /repo-旧 vs 空 cwd vs 大小写
```

### 4. 快照失效与大结果集

```text
首查：按完整查询指纹生成并缓存有序结果，最多 500 条，TTL 10 分钟
翻页：cursor 只携带 snapshot_id + offset；命中缓存则按偏移取回
缓存缺失/重启/TTL 到期：忽略 cursor，从头查询并加说明行
超过 500 条：返回前 500 条并明确提示缩小查询，不把截断结果称为完整结果
```

## 迁移与兼容

- **S1 无迁移**：不加表、不加列、不改索引；`EntrySearch` 新字段都是零值可用（旧调用行为不变）。
- **S2 不在本 Draft 的迁移范围内**；未来设计必须另行定义迁移与回填边界。
- 回填耗时（实测外推）：纯文本插入 + 建 trigram 索引 10 万行（9.6 MB 文本）用 4.4s、建 bigram 索引 4.9s，两者同量级 → 回填成本 ≈ 再建一遍索引。真实库体积先测（见测试与基准）：计划里必须包含"用真实 `sessions.db` 副本测回填耗时与体积增长"。
- 回滚：
  - S1：`git revert` 该提交即可（无数据变化）。
- 兼容：`session_search` 的新参数都是可选；结果文本只在还有下一页时多一行 `next_cursor:`，模型与既有测试的断言（含 `ledger_test.go:386-388`）不受影响。CLI `suga sessions --query` 走同一个 store 接口，行为随其存储层而变化（可选地暴露 `--kind`）。

## 安全、数据、运维与回滚

- 安全：检索是只读工具（`ReadOnly: true`，`internal/personal/store` 仅执行参数化的只读查询）；新增 SQL 全部参数化（`Kinds` 用占位符、时间与路径用绑定参数），不留拼接口子；`Cursor` 是**不可信输入**，解析失败一律忽略（`v1.` 前缀 + 固定字段数 + 白名单字符），不参与 SQL 拼接，只用于查找进程内快照。
- 数据：S1 不新增派生索引，不修改 `entries`/`sessions`；进程内快照不落盘。未来 S2 的派生索引与清理策略必须另案定义。
- 运维：本 Draft 无后台回填与新索引；S1 的快照只存在进程内，TTL 10 分钟且可因内存压力淘汰。
- 隐私：结果仍整体过 `redact.Clean`（`pkg/agentcore/tools/native/ledger.go:122`）；游标设计为纯文本短 token，测试断言"游标过 `redact.Clean` 后不变"。
- 风险：未来 S2 的索引体积与写放大必须在真实库上量化；Windows 非 ASCII 路径大小写折叠的已知差异（D7）；`snippet()` 的 64-token 窗口在 trigram 下约等于 64 rune（`fts.go:127-128` 注释），改造后不要改变它。

## 决策

- **D1 不换 tokenizer**：`unicode61` 对连续 CJK 只能整段匹配，直接排除；`trigram` 继续承担 ≥3 rune 的词，短词走 LIKE。
- **D2 短词不再让整条查询退化**（S1）：按词分流，长词走 `MATCH`、短词作为 `LIKE` 过滤条件，同一条 SQL 用 `AND` 组合。理由：修掉 issue 指出的"整条退化"，且零迁移。备选：保持现状只做分页/过滤——把最常见的 2 字中文查询留在无排序的全表扫描上，弃。
- **D3 短短语绝不进 `MATCH`**：实测显示短短语会让约束被静默丢弃（隐式 AND）或整体空（显式 AND），两种都不可接受。加两条守卫测试：断言"进 MATCH 的词全部 ≥3 rune"、断言"混合查询不会返回只满足长词的行"。
- **D4 S2 不属于本 Draft**：未来若立项，必须先按后续方向中的边界、并发、稳定 rowid 与版本闸门条件另起设计。
- **D5 游标用纯文本编码**（`v1.<snapshot_id>.<offset>`），不用 base64/JSON；它短、可读且不会把排序键暴露为可漂移状态。解析失败或快照不存在时忽略并从头查询。
- **D6 进程内快照分页**：首次查询固定有序 `(session_id, seq)` 列表，游标只保存快照 id 与偏移；不把 bm25 或时间作为跨页游标键。
- **D7 cwd 条件下推，Windows 用 `lower()`（ASCII 折叠）**：与现状 `strings.ToLower` 的差异仅限"路径含大小写不同的非 ASCII 字符"，接受并在测试里固定。备选：SQL 粗筛 + Go 对候选会话精判（语义完全等价，但要重新引入"先取 id 列表"的步骤，收益是极少数路径的精度），列为 Q3。
- **D8 S1 不依赖新增索引**：所有 1–2 rune 词稳定走 LIKE，未来辅助索引另案设计，不能改变本 Draft 的正确性语义。
- **D9 `recall` 不引入游标**：它已有 `offset`/`limit`（`maxRecallOffset = 100000`，`ledger.go:34-35、255-290`），模型已经会用；只给 `session_search` 加游标。备选：统一成游标——收益低、提示词与测试面变大，弃。
- **D10 kind 用逗号分隔的多值字符串**：合法值为 `user|assistant|tool|compaction`，schema 不使用 enum，服务端校验并拒绝空值/未知值。备选 JSON 数组弃用。
- **D11 时间参数接受相对与绝对两种写法**：`7d`/`36h` 与 `2026-10-01`/RFC3339；`since` 含、`until` 不含（半开），并在描述里写明。
- **D12 本 Draft 不新增索引表**：S1 不改 schema；未来 S2 必须另行确定稳定 rowid 与清理策略。
- **D13 保留 `CWDPrefix` 字段**：它现在是“裸字符串前缀、大小写敏感”（`fts.go:80-82、151-155`），测试仍在用；新字段 `WorkspaceDir/CurrentSessionID/FoldCase` 与它并存，本次不改它的语义。`SessionCwds` 在 ledger 路径停用；若确认无其它调用者则由计划决定是否删除（`ledger.go:111` 是唯一调用者）。

## session_search / recall 参数变化对模型提示的影响

- `session_search` schema（`pkg/agentcore/tools/native/ledger.go:92-108`）新增三个可选参数：`cursor`（string）、`kind`（逗号分隔 string，服务端校验）、`since`/`until`（string）。description 必须回答"游标从哪来"：`"Paging: when the result ends with a next_cursor line, pass that value back verbatim as cursor; a cursor from a different query or scope is ignored."`
- 描述里补一句中文行为提示：`"Chinese queries: a single character does not use the index (slow); two characters use it once the bigram index is ready, otherwise a scan."`（S2 完成后改写为不含"otherwise"的版本。）这句话是为模型行为服务的（鼓励 2 字以上查询），必须简短。
- 上下文成本：工具描述每次请求都随 schema 发送。新增文字控制在 ~200 字符内，并在计划里核对 `suga manifest`/`turn_context` 的 tools 字符数变化（`internal/personal/trace.go:462-488` 的 `context` 事件记录了 `tools=` 字符数，可作为回归口径）。
- 结果文本：`renderLedgerHits`（`ledger.go:154-176`）末尾追加 `next_cursor: <…>`（仅有下一页时）。既有断言（`internal/personal/ledger_test.go:386-388`、`store/fts_test.go:160-234` 的子用例）必须继续通过；新增断言包含 `next_cursor` 行与"末页无该行"。
- `recall` 不变（描述与 schema 都不动）——减少对模型行为的影响面。
- ACP/MCP 无需改动：工具分类仍是 `ToolKindSearch`（`internal/acpserver/policy.go:368`）。

## 测试与基准口径

单元测试（`store/fts_test.go` 扩展 + 新增等价性测试，都用 `t.TempDir()` 真实库；断言用 `github.com/gookit/goutil/x/assert`，用例用 `t.Run`）：

1. **中文分词行为**：1 字（`中`）走 LIKE；2 字（`缓存`）走 LIKE 且**结果与旧实现逐例相同**；3 字（`数据库`）走 trigram；混合（`中文 token`）断言"不返回只含 token 的行"（反例：构造一条只有 token 的 entry）。
2. **语法字面量**：扩展现有 `FTS syntax characters are literal` 用例，包含 `"`、`-`、`.`、`*`、`(`、`，`、`！`；断言"进 MATCH 的词都 ≥3 rune"（守卫 D3）。
3. **分页**：构造 45 条命中，`limit=20` 连续三次取满 → 三次结果的并集 == 一次性 `limit=100` 的结果，无重复、无遗漏；`next_cursor` 在末页为空；游标与 query/scope/kind 不匹配 → 从头开始且结果里有说明行；游标字段非法（缺字段、多字段、坏字符）→ 忽略而不报错。
4. **过滤**：`kind`（四类取值 + 非法值报错）、`since`/`until`（相对与绝对、边界含/不含）、`scope`（current/workspace/all）、`IncludeAgents`；过滤与分页同时使用的组合用例。
5. **cwd 下推等价性**（最重要的守卫）：把旧实现（`SessionCwds` + `pathWithin`）保留为测试内的参照函数，对一组会话目录（`/repo`、`/repo2`、`/repo/sub`、`/repo-旧`、`C:\Repo`、空 cwd、"当前会话无 cwd"）逐例断言"新 SQL 下推的命中集合 == 参照实现"，平台折叠用参数注入（不依赖真实 GOOS）。
6. **redact 与游标**：`redact.Clean(next_cursor)` == 原值；`session_search` 的渲染结果与游标共存的端到端用例（`internal/personal/ledger_test.go` 风格）。
7. **快照边界**：构造超过 500 条命中，断言结果明确提示已截断且快照最多保留 500 条；连续分页无重复无遗漏；TTL 到期、进程内缓存缺失和指纹不匹配均从头查询并给出说明。

基准（仓库首个 `func Benchmark`，`store/fts_bench_test.go`）：

- 语料：固定种子的中英混合生成器（与 `tmp/ftsprobe` 的语料同构，便于对照实测数字），两档规模（1 万 / 10 万条）。
- 用例：`trigram_hit`（3 rune 选择性）、`trigram_common`（3 rune 高频）、`like_2rune`（S1 的 2 字词）、`bigram_2rune`（S2）、`paginated_page2`（带游标）。
- 报告口径：`ns/op`；用 `b.ReportMetric` 附上"命中数"以便判读；索引体积与建索引用时不在 Go benchmark 里测，改用命令实测记录（已在本设计 §B 记录基线，未来 S2 立项时用真实库复测）。
- 计划阶段的验收项（必须做，写进计划）：在**真实 `sessions.db` 副本**上测：(a) 回填耗时；(b) 库体积增长；(c) 十条典型中文查询的 `EXPLAIN QUERY PLAN` 与实际耗时（1 字/2 字/3 字/混合/纯英文各两条）。

冒烟与真实模型：

- 检索是工具层行为，不需要伪终端场景；用 `internal/personal/ledger_test.go` 的端到端（runner + 工具调用）包含"模型调 `session_search` → 拿到 `next_cursor` → 带游标再调"的链路（假 provider 即可确定性断言）。
- 建议（AGENTS.md：涉 provider 协议才强制）加一轮真实 provider：让模型用一个 2 字中文词检索、按 `next_cursor` 翻一页，核对它把游标当字符串原样回传。做不到时在交付说明里写明。

## 待确认事项

- **Q1 S2（bigram 辅助索引）**：建议本期不做，另起设计并按真实库实测再决定；备选是用户明确批准后启动后续设计，但必须先满足本文列出的边界、并发、rowid 与版本闸门前提。
- **Q2 2 字英文词**：采纳评审建议，不纳入未来 CJK-only bigram；S1 继续 LIKE。是否需要英文专用索引另案。
- **Q3 cwd 折叠精度**：建议接受 `lower()`（ASCII）并在测试中固定；备选：SQL 粗筛 + Go 精判候选（语义完全等价，代价是重新引入一层"候选 id 列表"）。
- **Q4 分页语义**：采纳多取一条；只有确认有下一页才返回 `next_cursor`。
- **Q5 游标过期策略**：建议忽略不匹配的游标并从头发（并在结果里说明一行）；备选：报错让模型重写查询（更严格，但容易打断连续检索）。
- **Q6 `kind` 是否允许多值**：建议允许逗号分隔的多值；备选：只允许单值（schema 更简单）。
- **Q7 CLI 是否跟进**：建议 CLI 不跟进 cursor/kind/分页（检索主要给模型用，CLI 按会话去重且只显示首个 snippet）；备选：仅增加 `--kind`，但需要单独定义 CLI 的结果语义。
- **Q8 是否引入 `search.*` 配置键**：建议不增加配置键，500 条上限与 10 分钟 TTL 固定在实现中；备选是未来 S2 立项时再为迁移/回填定义配置，避免本期扩大配置面。


## 结论与人工计划 Gate

方案的核心是“按词分流”：把 trigram 用不了的词从 `MATCH` 表达式里拿出来，改成同一条 SQL 里的过滤条件；再用进程内有序快照与偏移游标提供稳定分页，同时补上 kind/时间过滤与 cwd 条件下推，S1 不需要改索引即可落地。设计的三条硬约束来自本次实测而不是推断：`minMatchRunes=3` 是硬下限、短短语进 `MATCH` 会被静默丢弃（必须守卫）、`unicode61` 对连续 CJK 无效（排除该路线）。

下一步：请用户就 Q1–Q8 给出选择（Q1 决定后续 S2 是否另案）→ 起草 `docs/plans/2026-10-16-session-search-cjk-plan.md`（建议先实施 S1；S2 另起设计并先做真实库实测）。批准设计不授权实施。
