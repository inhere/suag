package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Ledger full-text index (D8).
//
// entries_fts (migration 20261010-234359-add-ledger-fts-memory-meta.sql) is an
// FTS5 trigram table with its own rowid; each row points at its entry through
// the session_id/seq columns. It is written in two places that MUST derive the
// same role/text from an entry:
//
//   - the migration's one-shot backfill of the existing ledger (SQL literal);
//   - AppendEntries, for every new entry, using the constants below.
//
// Any change to ftsIndexedTypes, ftsRoleExpr or ftsTextExpr has to be copied
// into a NEW migration (applied migrations never re-run);
// TestFTSExpressionsMatchMigration fails when the two copies drift.
const (
	// ftsIndexedTypes lists the entry types worth searching: conversation
	// messages (EntryUser, EntryAssistant, EntryTool) and compaction summaries
	// (EntryCompaction, the only text left of compacted history). Plan,
	// handoff and context_update records are structured state, not prose.
	ftsIndexedTypes = `'user', 'assistant', 'tool', 'compaction'`

	// ftsRoleExpr stores the entry type as the role.
	ftsRoleExpr = `type`

	// ftsTextExpr extracts the searchable text from entries.json:
	//   - invalid JSON yields '' (json_extract would raise and fail the write);
	//   - compaction records ({"summary": ...}) index their summary;
	//   - api.Message JSON (no json tags) indexes Content, plus
	//     ToolResult.Content when it adds something: a tool message carries the
	//     same stored result in both fields, so equal values are kept once.
	// Surrounding whitespace is trimmed; rows whose text ends up '' are skipped.
	ftsTextExpr = `CASE
            WHEN NOT json_valid(json) THEN ''
            WHEN type = 'compaction' THEN trim(coalesce(json_extract(json, '$.summary'), ''), char(32, 9, 10, 13))
            ELSE trim(coalesce(json_extract(json, '$.Content'), '') ||
                CASE WHEN coalesce(json_extract(json, '$.ToolResult.Content'), '') IN ('', coalesce(json_extract(json, '$.Content'), ''))
                    THEN '' ELSE char(10) || json_extract(json, '$.ToolResult.Content') END, char(32, 9, 10, 13))
        END`
)

// ftsSourceSelect selects (session_id, seq, role, text) of the indexable
// entries; extra is appended to the inner WHERE (e.g. " AND seq = ?").
func ftsSourceSelect(extra string) string {
	return `SELECT session_id, seq, role, text FROM (SELECT session_id, seq, ` + ftsRoleExpr + ` AS role, ` +
		ftsTextExpr + ` AS text FROM entries WHERE type IN (` + ftsIndexedTypes + `)` + extra + `) WHERE text <> ''`
}

// ftsInsertEntrySQL indexes one just-inserted entry (args: session_id, seq).
// It is a no-op for non-indexed types and entries without text.
var ftsInsertEntrySQL = `INSERT INTO entries_fts(session_id, seq, role, text) ` +
	ftsSourceSelect(` AND session_id = ? AND seq = ?`)

// Search limits and the LIKE-path snippet radius (runes on each side).
const (
	defaultSearchLimit = 20
	maxSearchLimit     = 100
	snippetRadius      = 60
	// minMatchRunes is the shortest term the trigram tokenizer can match.
	minMatchRunes = 3
)

// EntrySearch is a ledger full-text query.
type EntrySearch struct {
	// Query is split on whitespace; every term must occur (AND). Terms are
	// literal text: FTS5 syntax characters have no special meaning.
	Query string
	// SessionIDs restricts the search to these sessions; empty means all.
	SessionIDs []string
	// CWDPrefix keeps only sessions whose cwd starts with it (plain string
	// prefix, case-sensitive); empty means no restriction.
	CWDPrefix string
	// Limit caps the hits: <=0 means 20, at most 100.
	Limit int
	// IncludeAgents searches sub-agent sessions too (they belong to their
	// parent session and are left out by default).
	IncludeAgents bool
}

// EntryHit is one matching entry, located by SessionID/Seq.
type EntryHit struct {
	SessionID string
	Seq       int64
	Role      string
	Snippet   string
	CreatedAt time.Time
}

// SearchEntries searches the ledger index. When every term has at least three
// runes it uses the trigram index (FTS5 MATCH, bm25 ranking, snippet());
// otherwise the whole query falls back to LIKE over the indexed text (newest
// first). Results are not redacted here; callers do that.
func (s *Store) SearchEntries(ctx context.Context, q EntrySearch) ([]EntryHit, error) {
	terms := strings.Fields(q.Query)
	if len(terms) == 0 {
		return nil, errors.New("search query is required")
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	limit = min(limit, maxSearchLimit)

	useMatch := true
	for _, t := range terms {
		if utf8.RuneCountInString(t) < minMatchRunes {
			useMatch = false
			break
		}
	}

	var (
		cols, where, order string
		args               []any
	)
	if useMatch {
		// Column 3 is text; up to 64 tokens (trigram: ~64 runes) of context.
		cols = `snippet(entries_fts, 3, '', '', '…', 64)`
		where = `entries_fts MATCH ?`
		args = append(args, ftsMatchExpr(terms))
		order = `bm25(entries_fts), f.session_id, f.seq`
	} else {
		cols = `f.text`
		likes := make([]string, len(terms))
		for i, t := range terms {
			likes[i] = `f.text LIKE ? ESCAPE '\'`
			args = append(args, "%"+escapeLike(t)+"%")
		}
		where = strings.Join(likes, ` AND `)
		order = `e.created_at DESC, f.session_id, f.seq DESC`
	}
	if len(q.SessionIDs) > 0 {
		where += ` AND f.session_id IN (` + placeholders(len(q.SessionIDs)) + `)`
		for _, id := range q.SessionIDs {
			args = append(args, id)
		}
	}
	if !q.IncludeAgents {
		where += ` AND ` + notAgentSession("ss.meta_json")
	}
	if q.CWDPrefix != "" {
		// substr/length instead of LIKE: exact, case-sensitive, no escaping.
		where += ` AND substr(ss.cwd, 1, length(?)) = ?`
		args = append(args, q.CWDPrefix, q.CWDPrefix)
	}
	// MATCH, snippet() and bm25() take the FTS hidden column, which is named
	// after the table (entries_fts) and resolves through the alias f.
	sqlText := `SELECT f.session_id, f.seq, f.role, ` + cols + `, e.created_at
		FROM entries_fts AS f
		JOIN entries AS e ON e.session_id = f.session_id AND e.seq = f.seq
		JOIN sessions AS ss ON ss.id = f.session_id
		WHERE ` + where + ` ORDER BY ` + order + fmt.Sprintf(` LIMIT %d`, limit)
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("search entries: %w", err)
	}
	defer rows.Close()
	out := []EntryHit{}
	for rows.Next() {
		var (
			h       EntryHit
			text    string
			created string
		)
		if err := rows.Scan(&h.SessionID, &h.Seq, &h.Role, &text, &created); err != nil {
			return nil, err
		}
		h.CreatedAt = parseTime(created)
		if useMatch {
			h.Snippet = text
		} else {
			h.Snippet = likeSnippet(text, terms[0])
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ftsMatchExpr turns each term into an FTS5 string (phrase): wrapped in double
// quotes with inner quotes doubled, so `*`, `OR`, `NEAR(`, `-`, `:` ... are
// plain text. Space-separated phrases are an implicit AND.
func ftsMatchExpr(terms []string) string {
	quoted := make([]string, len(terms))
	for i, t := range terms {
		quoted[i] = `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
	}
	return strings.Join(quoted, " ")
}

// escapeLike escapes the LIKE wildcards (and the escape char itself) for
// `LIKE ? ESCAPE '\'`.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// likeSnippet cuts text to the first occurrence of term with snippetRadius
// runes on each side. Matching folds case per rune, like SQLite LIKE does for
// ASCII, and keeps rune indexes aligned with text.
func likeSnippet(text, term string) string {
	rs, ts := []rune(text), []rune(term)
	at := indexFold(rs, ts)
	if at < 0 { // cannot happen for a LIKE hit; fall back to the head
		at = 0
	}
	start, end := max(at-snippetRadius, 0), min(at+len(ts)+snippetRadius, len(rs))
	out := string(rs[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(rs) {
		out += "…"
	}
	return out
}

// indexFold returns the rune index of the first case-insensitive occurrence of
// sub in s, or -1.
func indexFold(s, sub []rune) int {
	if len(sub) == 0 {
		return 0
	}
outer:
	for i := 0; i+len(sub) <= len(s); i++ {
		for j, r := range sub {
			if unicode.ToLower(s[i+j]) != unicode.ToLower(r) {
				continue outer
			}
		}
		return i
	}
	return -1
}
