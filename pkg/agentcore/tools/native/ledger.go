package native

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/inhere/suag/pkg/agentcore/api"
	"github.com/inhere/suag/pkg/agentcore/redact"
	"github.com/inhere/suag/pkg/agentcore/tools"
)

// Ledger search scopes (session_search scope argument).
const (
	LedgerScopeCurrent   = "current"
	LedgerScopeWorkspace = "workspace"
	LedgerScopeAll       = "all"
)

// OutputRefPrefix starts every ref to an externalised tool output
// (out:<session_id>:<16 hex>); other refs point at ledger messages
// (<session_id>/<seq>).
const OutputRefPrefix = "out:"

// LedgerQuery is one session_search request.
type LedgerQuery struct {
	// Query is split on whitespace; every term must occur (AND). Terms are
	// literal text, never search syntax.
	Query string
	// Scope is current, workspace or all ("" = workspace).
	Scope string
	// Ref, when set, is an out:... ref: the search then scans that one
	// externalised output line by line and Scope is ignored.
	Ref string
	// Limit caps the hits (<= 0 = the implementation default).
	Limit int
}

// LedgerHit is one search match.
type LedgerHit struct {
	// Ref locates the match: <session_id>/<seq> for a message, the out:...
	// ref for a line of an externalised output.
	Ref string
	// Role is the entry type of a message hit (user, assistant, tool,
	// compaction); empty for line hits.
	Role string
	// Line is the 1-based line number of a hit inside an externalised output;
	// 0 for message hits.
	Line int
	// Snippet is a short excerpt around the match.
	Snippet string
}

// LedgerSearcher reads the session ledger and externalised tool outputs. The
// host implements it (one real implementation, so the interface is declared
// here at the consumer, like PlanSink and ToolSearcher).
type LedgerSearcher interface {
	Search(ctx context.Context, q LedgerQuery) ([]LedgerHit, error)
	// Fetch returns a readable window: for <session_id>/<seq> the messages
	// from seq on (offset skips messages, limit counts them); for out:... the
	// lines starting at line offset (1-based; 0 = 1), limit lines.
	Fetch(ctx context.Context, ref string, offset, limit int) (string, error)
}

// SessionSearchTool searches earlier conversation history and externalised
// outputs. Results are old, possibly external content: the tool is External
// so the session is flagged as tainted when it is used.
type SessionSearchTool struct {
	searcher LedgerSearcher
}

// NewSessionSearchTool creates session_search backed by searcher.
func NewSessionSearchTool(searcher LedgerSearcher) *SessionSearchTool {
	return &SessionSearchTool{searcher: searcher}
}

func (t *SessionSearchTool) Name() string { return "session_search" }
func (t *SessionSearchTool) Description() string {
	return "Full-text search of earlier conversation history (messages, tool results and compaction summaries). " +
		"Every whitespace-separated word must occur; words are matched literally. scope: workspace (default, " +
		"sessions of this workspace), current (this session) or all. Each hit is `<ref>  <role>  <snippet>`; " +
		"read around a hit with recall(ref). With ref=out:<session>:<hex> it instead searches that saved tool " +
		"output line by line (case-insensitive) and returns `<ref>  L<line>  <text>`."
}

// ToolMeta: read-only search; results are external content (taint).
func (*SessionSearchTool) ToolMeta() tools.Meta {
	return tools.Meta{Category: tools.CategorySearch, ReadOnly: true, External: true}
}

func (t *SessionSearchTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Words to find (all must occur)"},
				"scope": map[string]any{"type": "string", "enum": []any{LedgerScopeWorkspace, LedgerScopeCurrent, LedgerScopeAll},
					"description": "Sessions to search (default workspace); ignored with ref"},
				"ref":   map[string]any{"type": "string", "description": "Optional out:<session>:<hex> ref: search inside that saved output"},
				"limit": map[string]any{"type": "integer", "description": "Max hits (default 20, at most 100)"},
			},
			"required": []any{"query"},
		},
	}
}

func (t *SessionSearchTool) Execute(ctx context.Context, call api.ToolCall) (*api.ToolResult, error) {
	q, err := parseLedgerQuery(call.Arguments)
	if err != nil {
		return nil, fmt.Errorf("session_search: %w", err)
	}
	if t.searcher == nil {
		return nil, errors.New("session_search: no ledger configured")
	}
	hits, err := t.searcher.Search(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("session_search: %w", err)
	}
	return &api.ToolResult{CallID: call.CallID, Name: t.Name(), Content: redact.Clean(renderLedgerHits(q, hits))}, nil
}

// parseLedgerQuery validates the session_search arguments.
func parseLedgerQuery(args map[string]any) (LedgerQuery, error) {
	var q LedgerQuery
	q.Query, _ = args["query"].(string)
	q.Query = strings.TrimSpace(q.Query)
	if q.Query == "" {
		return q, errors.New("query is required")
	}
	scope, _ := args["scope"].(string)
	switch scope = strings.TrimSpace(scope); scope {
	case "":
		q.Scope = LedgerScopeWorkspace
	case LedgerScopeCurrent, LedgerScopeWorkspace, LedgerScopeAll:
		q.Scope = scope
	default:
		return q, fmt.Errorf("scope %q must be current, workspace or all", scope)
	}
	ref, _ := args["ref"].(string)
	if q.Ref = strings.TrimSpace(ref); q.Ref != "" && !strings.HasPrefix(q.Ref, OutputRefPrefix) {
		return q, fmt.Errorf("ref %q: only out:<session>:<hex> refs can be searched; read a message ref with recall", q.Ref)
	}
	if n, ok := numberArg(args["limit"]); ok && n >= 1 {
		q.Limit = int(n)
	}
	return q, nil
}

// renderLedgerHits formats hits one per line; snippets are flattened to a
// single line so a hit can never look like several.
func renderLedgerHits(q LedgerQuery, hits []LedgerHit) string {
	if len(hits) == 0 {
		if q.Ref != "" {
			return fmt.Sprintf("No line of %s matched %q.", q.Ref, q.Query)
		}
		return fmt.Sprintf("No message matched %q in scope %s.", q.Query, q.Scope)
	}
	var b strings.Builder
	if q.Ref != "" {
		fmt.Fprintf(&b, "%d matching line(s) in %s; read around one with recall(ref, offset=<line>):\n", len(hits), q.Ref)
	} else {
		fmt.Fprintf(&b, "%d hit(s) in scope %s; read one with recall(ref):\n", len(hits), q.Scope)
	}
	for _, h := range hits {
		snippet := strings.Join(strings.Fields(h.Snippet), " ")
		if h.Line > 0 {
			fmt.Fprintf(&b, "%s  L%d  %s\n", h.Ref, h.Line, snippet)
		} else {
			fmt.Fprintf(&b, "%s  %s  %s\n", h.Ref, h.Role, snippet)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// RecallTool reads a window of earlier history or of an externalised output
// by ref. It pages on its own (Rereadable): its result is capped at the
// inline threshold here, so the output layer never externalises it again.
type RecallTool struct {
	searcher LedgerSearcher
	maxOut   int // inline threshold in bytes; 0 = DefaultMaxOutputBytes
}

// NewRecallTool creates recall backed by searcher; maxOut caps one result
// (normally the inline threshold, tools.max_output_bytes).
func NewRecallTool(searcher LedgerSearcher, maxOut int) *RecallTool {
	return &RecallTool{searcher: searcher, maxOut: maxOut}
}

func (t *RecallTool) Name() string { return "recall" }
func (t *RecallTool) Description() string {
	return "Read earlier history by ref. A message ref <session>/<seq> (from session_search) returns the " +
		"messages from that seq on: offset skips messages, limit counts them (default 20, at most 100). " +
		"An output ref out:<session>:<hex> (a saved large tool output) returns numbered lines: offset is " +
		"the 1-based start line, limit the line count (default 200, at most 2000)."
}

// ToolMeta: read-only, pages on its own, results are external content.
func (*RecallTool) ToolMeta() tools.Meta {
	return tools.Meta{Category: tools.CategorySearch, ReadOnly: true, External: true, Rereadable: true}
}

func (t *RecallTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"ref":    map[string]any{"type": "string", "description": "<session>/<seq> or out:<session>:<hex>"},
				"offset": map[string]any{"type": "integer", "description": "Messages to skip, or 1-based start line for out: refs"},
				"limit":  map[string]any{"type": "integer", "description": "Messages (default 20) or lines (default 200) to return"},
			},
			"required": []any{"ref"},
		},
	}
}

func (t *RecallTool) Execute(ctx context.Context, call api.ToolCall) (*api.ToolResult, error) {
	ref, _ := call.Arguments["ref"].(string)
	if ref = strings.TrimSpace(ref); ref == "" {
		return nil, errors.New("recall: ref is required")
	}
	offset, limit := 0, 0
	if n, ok := numberArg(call.Arguments["offset"]); ok {
		if n < 0 {
			return nil, errors.New("recall: offset must not be negative")
		}
		offset = int(n)
	}
	if n, ok := numberArg(call.Arguments["limit"]); ok && n >= 1 {
		limit = int(n)
	}
	if t.searcher == nil {
		return nil, errors.New("recall: no ledger configured")
	}
	text, err := t.searcher.Fetch(ctx, ref, offset, limit)
	if err != nil {
		return nil, fmt.Errorf("recall: %w", err)
	}
	text = redact.Clean(text)
	// Whole lines only, so the last line shown tells where to continue.
	if kept, _, cut := TruncateLines(text, outputLimit(t.maxOut)); cut {
		text = strings.TrimRight(kept, "\n") + fmt.Sprintf("\n[output cut at %d bytes; continue after the last line shown "+
			"(a later offset, or the next message ref) or pass a smaller limit]", outputLimit(t.maxOut))
	}
	return &api.ToolResult{CallID: call.CallID, Name: t.Name(), Content: text}, nil
}
