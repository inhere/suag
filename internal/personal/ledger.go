package personal

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/redact"
	"github.com/inhere/suag/pkg/agentcore/tools/native"
)

// Ledger window limits.
const (
	defaultRecallMessages = 20
	maxRecallMessages     = 100
	defaultRecallLines    = 200
	maxRecallLines        = 2000
	defaultLineHits       = 20
	maxLineHits           = 100
	// maxRecallOffset bounds the message offset (it becomes a SQL LIMIT).
	maxRecallOffset = 100000
	// maxMessageBytes caps one rendered message body in a recall window.
	maxMessageBytes = 4000
	// maxLineBytes caps one line of an externalised output in results.
	maxLineBytes = 2000
	// maxToolArgBytes caps the rendered arguments of a tool call.
	maxToolArgBytes = 500
)

// Refs: a session id may be any id validateSessionID accepts (custom ids from
// --session-id included, not only sess_...); none of its characters is ':' or
// '/', so both forms split unambiguously. The hex part is exactly what
// OutputFileName produces (lowercase).
var (
	outputRefPattern  = regexp.MustCompile(`^out:([A-Za-z0-9._-]+):([0-9a-f]{16})$`)
	messageRefPattern = regexp.MustCompile(`^([A-Za-z0-9._-]+)/([1-9][0-9]{0,17})$`)
)

// ledgerMessageTypes are the entry types a recall window shows (the same
// types the FTS index holds).
var ledgerMessageTypes = []string{store.EntryUser, store.EntryAssistant, store.EntryTool, store.EntryCompaction}

// Ledger implements native.LedgerSearcher over the session store and the
// sessions directory (externalised outputs). Like PlanRecorder it is created
// before the Runner and bound to the Runner's store in New (Config.Ledger).
type Ledger struct {
	mu   sync.Mutex
	repo *store.Store
	// dir is the sessions root (<SUAG_HOME>/sessions).
	dir string
	// foldCase compares workspace paths case-insensitively (Windows).
	foldCase bool
}

var _ native.LedgerSearcher = (*Ledger)(nil)

// NewLedger creates an unbound ledger reading outputs under sessionsDir.
func NewLedger(sessionsDir string) *Ledger {
	return &Ledger{dir: sessionsDir, foldCase: runtime.GOOS == "windows"}
}

func (l *Ledger) bind(repo *store.Store) {
	l.mu.Lock()
	l.repo = repo
	l.mu.Unlock()
}

func (l *Ledger) store() (*store.Store, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.repo == nil {
		return nil, errors.New("ledger is not bound to a session store")
	}
	return l.repo, nil
}

// Search runs a ledger full-text search, or a line scan of one externalised
// output when q.Ref is set. Snippets are cleaned (redact.Clean).
func (l *Ledger) Search(ctx context.Context, q native.LedgerQuery) ([]native.LedgerHit, error) {
	if q.Ref != "" {
		return l.searchOutput(ctx, q)
	}
	repo, err := l.store()
	if err != nil {
		return nil, err
	}
	search := store.EntrySearch{Query: q.Query, Limit: q.Limit}
	current := observeScopeFrom(ctx).sessionID
	switch q.Scope {
	case native.LedgerScopeAll:
	case native.LedgerScopeCurrent, native.LedgerScopeWorkspace, "":
		if current == "" {
			return nil, fmt.Errorf("scope %s needs a current session; use scope all", scopeName(q.Scope))
		}
		search.SessionIDs = []string{current}
		if q.Scope != native.LedgerScopeCurrent {
			if search.SessionIDs, err = l.workspaceSessions(ctx, repo, current); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("scope %q must be current, workspace or all", q.Scope)
	}
	// An empty SessionIDs list means "all sessions" to the store; a scoped
	// search always includes at least the current session, but never let an
	// empty list widen the search.
	if q.Scope != native.LedgerScopeAll && len(search.SessionIDs) == 0 {
		return []native.LedgerHit{}, nil
	}
	hits, err := repo.SearchEntries(ctx, search)
	if err != nil {
		return nil, err
	}
	out := make([]native.LedgerHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, native.LedgerHit{
			Ref: messageRef(h.SessionID, h.Seq), Role: h.Role, Snippet: redact.Clean(h.Snippet),
		})
	}
	return out, nil
}

func scopeName(scope string) string {
	if scope == "" {
		return native.LedgerScopeWorkspace
	}
	return scope
}

// workspaceSessions returns the current session plus every session whose cwd
// equals the current session's cwd or lies below it. Matching respects path
// separators (/repo never matches /repo2), which the store's plain string
// prefix cannot. Without a recorded cwd the scope is the current session.
func (l *Ledger) workspaceSessions(ctx context.Context, repo *store.Store, current string) ([]string, error) {
	sess, err := repo.GetSession(ctx, current)
	if err != nil {
		return nil, fmt.Errorf("load current session: %w", err)
	}
	ids := []string{current}
	if strings.TrimSpace(sess.Cwd) == "" {
		return ids, nil
	}
	cwds, err := repo.SessionCwds(ctx)
	if err != nil {
		return nil, err
	}
	for id, cwd := range cwds {
		if id != current && pathWithin(sess.Cwd, cwd, l.foldCase) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// pathWithin reports whether p is dir or lies below it, comparing cleaned
// paths element-wise at separator boundaries (case-insensitively with fold).
func pathWithin(dir, p string, fold bool) bool {
	dir, p = filepath.Clean(dir), filepath.Clean(p)
	if fold {
		dir, p = strings.ToLower(dir), strings.ToLower(p)
	}
	if p == dir {
		return true
	}
	prefix := dir
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	return strings.HasPrefix(p, prefix)
}

func messageRef(sessionID string, seq int64) string {
	return sessionID + "/" + strconv.FormatInt(seq, 10)
}

// Fetch returns a cleaned window of a message ref (<session_id>/<seq>) or of
// an output ref (out:<session_id>:<hex>).
func (l *Ledger) Fetch(ctx context.Context, ref string, offset, limit int) (string, error) {
	if strings.HasPrefix(ref, outputRefPrefix) {
		sid, name, err := parseOutputRef(ref)
		if err != nil {
			return "", err
		}
		text, err := l.readOutputLines(ctx, ref, sid, name, offset, limit)
		return redact.Clean(text), err
	}
	sid, seq, err := parseMessageRef(ref)
	if err != nil {
		return "", err
	}
	text, err := l.readMessages(ctx, sid, seq, offset, limit)
	return redact.Clean(text), err
}

// parseOutputRef strictly validates out:<session_id>:<16 hex> and returns
// the session id and the file stem. Anything else (path separators, '..',
// extra ':', upper-case hex, absolute paths) is rejected before any file
// system access.
func parseOutputRef(ref string) (sessionID, name string, err error) {
	m := outputRefPattern.FindStringSubmatch(ref)
	if m == nil {
		return "", "", fmt.Errorf("invalid output ref %q: expected out:<session_id>:<16 lowercase hex>", ref)
	}
	if err := validateSessionID(m[1]); err != nil {
		return "", "", fmt.Errorf("invalid output ref %q: %w", ref, err)
	}
	return m[1], m[2], nil
}

// parseMessageRef validates <session_id>/<seq> (seq >= 1).
func parseMessageRef(ref string) (string, int64, error) {
	m := messageRefPattern.FindStringSubmatch(ref)
	if m == nil {
		return "", 0, fmt.Errorf("invalid ref %q: expected <session_id>/<seq> or out:<session_id>:<16 hex>", ref)
	}
	if err := validateSessionID(m[1]); err != nil {
		return "", 0, fmt.Errorf("invalid ref %q: %w", ref, err)
	}
	seq, err := strconv.ParseInt(m[2], 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("invalid ref %q: %w", ref, err)
	}
	return m[1], seq, nil
}

// requireSession turns a missing session into a clear error.
func (l *Ledger) requireSession(ctx context.Context, sessionID string) error {
	repo, err := l.store()
	if err != nil {
		return err
	}
	if _, err := repo.GetSession(ctx, sessionID); err != nil {
		if errors.Is(err, store.ErrSessionNotFound) {
			return fmt.Errorf("session %q not found (deleted or never existed)", sessionID)
		}
		return err
	}
	return nil
}

// readMessages renders up to limit messages of the session starting at seq,
// after skipping offset of them.
func (l *Ledger) readMessages(ctx context.Context, sessionID string, seq int64, offset, limit int) (string, error) {
	if err := l.requireSession(ctx, sessionID); err != nil {
		return "", err
	}
	repo, err := l.store()
	if err != nil {
		return "", err
	}
	if limit <= 0 {
		limit = defaultRecallMessages
	}
	limit = min(limit, maxRecallMessages)
	offset = min(max(offset, 0), maxRecallOffset)
	entries, err := repo.ListEntries(ctx, sessionID, store.EntryFilter{SinceSeq: seq - 1, Types: ledgerMessageTypes, Limit: offset + limit + 1})
	if err != nil {
		return "", err
	}
	if len(entries) <= offset {
		return "", fmt.Errorf("no message at or after %s (offset %d)", messageRef(sessionID, seq), offset)
	}
	entries = entries[offset:]
	more := len(entries) > limit
	if more {
		entries = entries[:limit]
	}
	var b strings.Builder
	for _, e := range entries {
		renderEntry(&b, e)
	}
	if more {
		fmt.Fprintf(&b, "[more messages follow; continue with recall(ref=%q)]\n", messageRef(sessionID, entries[len(entries)-1].Seq+1))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// renderEntry writes one ledger entry as a header line
// "[<session>/<seq>] <role> ..." and its (capped) body.
func renderEntry(b *strings.Builder, e store.Entry) {
	ref := messageRef(e.SessionID, e.Seq)
	stamp := e.CreatedAt.Local().Format("2006-01-02 15:04")
	if e.Type == store.EntryCompaction {
		var rec compactionRecord
		_ = json.Unmarshal(e.JSON, &rec)
		fmt.Fprintf(b, "[%s] compaction summary (%s)\n%s\n\n", ref, stamp, capText(rec.Summary, maxMessageBytes))
		return
	}
	var msg agentcore.Message
	if err := json.Unmarshal(e.JSON, &msg); err != nil {
		fmt.Fprintf(b, "[%s] %s (unreadable entry)\n\n", ref, e.Type)
		return
	}
	switch {
	case msg.ToolResult != nil:
		status := ""
		if msg.ToolResult.IsError {
			status = ", error"
		}
		content := msg.ToolResult.Content
		if content == "" {
			content = msg.Content
		}
		fmt.Fprintf(b, "[%s] tool result %s (%s%s)\n%s\n\n", ref, msg.ToolResult.Name, stamp, status, capText(content, maxMessageBytes))
	case msg.ToolCall != nil:
		args, _ := json.Marshal(msg.ToolCall.Arguments)
		fmt.Fprintf(b, "[%s] %s tool call %s %s (%s)\n", ref, e.Type, msg.ToolCall.Name, capText(string(args), maxToolArgBytes), stamp)
		if strings.TrimSpace(msg.Content) != "" {
			b.WriteString(capText(msg.Content, maxMessageBytes) + "\n")
		}
		b.WriteString("\n")
	default:
		fmt.Fprintf(b, "[%s] %s (%s)\n%s\n\n", ref, e.Type, stamp, capText(msg.Content, maxMessageBytes))
	}
}

// capText cuts s to max bytes (UTF-8 safe) and says how much was dropped.
func capText(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + fmt.Sprintf(" …[%d more bytes truncated]", len(s)-cut)
}

// openOutput opens one externalised output through an os.Root on the
// session's outputs directory, so neither the file name nor a symlink can
// lead the read outside it.
func (l *Ledger) openOutput(ref, sessionID, name string) (*os.File, error) {
	if strings.TrimSpace(l.dir) == "" {
		return nil, errors.New("no sessions directory configured")
	}
	root, err := os.OpenRoot(SessionOutputsDir(l.dir, sessionID))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("output %s not found (session %q has no saved outputs)", ref, sessionID)
		}
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(name + ".txt")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("output %s not found", ref)
		}
		return nil, err
	}
	return f, nil
}

// forEachLine calls fn with each line (1-based number, without the line
// break) until fn returns false. Lines of any length are handled.
func forEachLine(r io.Reader, fn func(n int, line string) bool) error {
	br := bufio.NewReader(r)
	for n := 1; ; n++ {
		line, err := br.ReadString('\n')
		if line == "" && err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if !fn(n, strings.TrimRight(line, "\r\n")) {
			return nil
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// readOutputLines renders lines [offset, offset+limit) of an output with
// their numbers, plus a hint when more lines follow.
func (l *Ledger) readOutputLines(ctx context.Context, ref, sessionID, name string, offset, limit int) (string, error) {
	if err := l.requireSession(ctx, sessionID); err != nil {
		return "", err
	}
	f, err := l.openOutput(ref, sessionID, name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if offset < 1 {
		offset = 1
	}
	if limit <= 0 {
		limit = defaultRecallLines
	}
	limit = min(limit, maxRecallLines)
	var (
		b     strings.Builder
		last  int
		total int
	)
	err = forEachLine(f, func(n int, line string) bool {
		total = n
		if n >= offset && n < offset+limit {
			fmt.Fprintf(&b, "%d\t%s\n", n, capText(line, maxLineBytes))
			last = n
		}
		return true // keep counting so the hint can tell the total
	})
	if err != nil {
		return "", fmt.Errorf("read output %s: %w", ref, err)
	}
	if last == 0 {
		return "", fmt.Errorf("output %s has %d line(s); offset %d is past the end", ref, total, offset)
	}
	if last < total {
		fmt.Fprintf(&b, "[lines %d-%d of %d; continue with offset=%d]\n", offset, last, total, last+1)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// searchOutput scans one externalised output line by line: every query term
// must occur in the line (case-insensitive substring match).
func (l *Ledger) searchOutput(ctx context.Context, q native.LedgerQuery) ([]native.LedgerHit, error) {
	sid, name, err := parseOutputRef(q.Ref)
	if err != nil {
		return nil, err
	}
	terms := strings.Fields(strings.ToLower(q.Query))
	if len(terms) == 0 {
		return nil, errors.New("search query is required")
	}
	if err := l.requireSession(ctx, sid); err != nil {
		return nil, err
	}
	f, err := l.openOutput(q.Ref, sid, name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	limit := q.Limit
	if limit <= 0 {
		limit = defaultLineHits
	}
	limit = min(limit, maxLineHits)
	hits := []native.LedgerHit{}
	err = forEachLine(f, func(n int, line string) bool {
		lower := strings.ToLower(line)
		for _, t := range terms {
			if !strings.Contains(lower, t) {
				return true
			}
		}
		hits = append(hits, native.LedgerHit{Ref: q.Ref, Line: n, Snippet: redact.Clean(capText(line, maxLineBytes))})
		return len(hits) < limit
	})
	if err != nil {
		return nil, fmt.Errorf("search output %s: %w", q.Ref, err)
	}
	return hits, nil
}
