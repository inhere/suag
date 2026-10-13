package personal

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
)

//go:embed traceweb.html
var traceWebHTML []byte

// maxWebText caps one text (message, tool argument value, tool result) sent
// to the trace page; the full text stays in the store (suag export).
const maxWebText = 64 * 1024

// TraceView is what `suag trace --web` serves at /api/session: the session,
// its events, its transcript entries joined to them (tool results to tool
// events by call id, assistant messages to the model call that produced
// them) and the loop analysis.
type TraceView struct {
	Session  traceSession  `json:"session"`
	Events   []webEvent    `json:"events"`
	Entries  []traceEntry  `json:"entries"`
	Analysis TraceAnalysis `json:"analysis"`
}

type traceSession struct {
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	Cwd       string         `json:"cwd"`
	Status    string         `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	Meta      map[string]any `json:"meta,omitempty"`
}

type webEvent struct {
	ID               int64          `json:"id"`
	TurnID           string         `json:"turn_id,omitempty"`
	Kind             string         `json:"kind"`
	Name             string         `json:"name"`
	Status           string         `json:"status"`
	Start            time.Time      `json:"start"`
	DurationMs       int64          `json:"duration_ms"`
	TTFTMs           int64          `json:"ttft_ms,omitempty"`
	InputTokens      int64          `json:"input_tokens,omitempty"`
	OutputTokens     int64          `json:"output_tokens,omitempty"`
	CacheReadTokens  int64          `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64          `json:"cache_write_tokens,omitempty"`
	Error            string         `json:"error,omitempty"`
	Attrs            map[string]any `json:"attrs,omitempty"`
	// Entries are the transcript entries (by seq) belonging to the event: a
	// tool call and its result for a tool event, the reply for a model call.
	Entries []int64 `json:"entries,omitempty"`
}

type traceEntry struct {
	Seq       int64           `json:"seq"`
	TurnID    string          `json:"turn_id,omitempty"`
	Type      string          `json:"type"`
	Time      time.Time       `json:"time"`
	Role      string          `json:"role,omitempty"`
	Content   string          `json:"content,omitempty"`
	Reasoning string          `json:"reasoning,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Tool      string          `json:"tool,omitempty"`
	Args      map[string]any  `json:"args,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Batch     string          `json:"batch,omitempty"`
	Raw       json.RawMessage `json:"raw,omitempty"` // non-message entries (segment, plan, handoff, context update)
}

// BuildTraceView loads one session for the trace page.
func BuildTraceView(ctx context.Context, repo *store.Store, sessionID string) (*TraceView, error) {
	sess, err := repo.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("trace session %q: %w", sessionID, err)
	}
	events, err := repo.ListEvents(ctx, store.EventFilter{SessionID: sessionID})
	if err != nil {
		return nil, err
	}
	entries, err := repo.ListEntries(ctx, sessionID, store.EntryFilter{})
	if err != nil {
		return nil, err
	}
	view := &TraceView{
		// Older sessions stored the whole first prompt as the title.
		Session: traceSession{ID: sess.ID, Title: SessionTitle(sess.Title), Cwd: sess.Cwd, Status: sess.Status,
			CreatedAt: sess.CreatedAt, UpdatedAt: sess.UpdatedAt, Meta: sess.Meta},
		Analysis: AnalyzeTrace(events),
	}
	for _, e := range entries {
		view.Entries = append(view.Entries, webEntry(e))
	}
	for _, e := range events {
		view.Events = append(view.Events, webEvent{ID: e.ID, TurnID: e.TurnID, Kind: e.Kind, Name: e.Name,
			Status: e.Status, Start: e.StartedAt, DurationMs: e.DurationMs, TTFTMs: e.TTFTMs,
			InputTokens: e.InputTokens, OutputTokens: e.OutputTokens, CacheReadTokens: e.CacheReadTokens,
			CacheWriteTokens: e.CacheWriteTokens, Error: e.Error, Attrs: e.Attrs})
	}
	joinEntries(view)
	return view, nil
}

// webEntry turns a stored entry into its page form: messages parsed and
// their long texts capped, other entries passed as raw JSON.
func webEntry(e store.Entry) traceEntry {
	out := traceEntry{Seq: e.Seq, TurnID: e.TurnID, Type: e.Type, Time: e.CreatedAt}
	switch e.Type {
	case store.EntryUser, store.EntryAssistant, store.EntryTool:
		var m agentcore.Message
		if json.Unmarshal(e.JSON, &m) != nil {
			out.Raw = e.JSON
			return out
		}
		out.Role, out.Content, out.Reasoning = m.Role, capWebText(m.Content, maxWebText), capWebText(m.Reasoning, maxWebText)
		if m.ToolCall != nil {
			out.CallID, out.Tool = m.ToolCall.CallID, m.ToolCall.Name
			if m.ToolResult == nil {
				out.Args = capArgs(m.ToolCall.Arguments)
			}
		}
		if m.ToolResult != nil {
			out.CallID, out.Tool, out.IsError = m.ToolResult.CallID, m.ToolResult.Name, m.ToolResult.IsError
			out.Content = capWebText(m.ToolResult.Content, maxWebText)
		}
		out.Batch, _ = m.Metadata[agentcore.ToolBatchMetaKey].(string)
	default:
		out.Raw = e.JSON
	}
	return out
}

func capArgs(args map[string]any) map[string]any {
	if len(args) == 0 {
		return nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		if s, ok := v.(string); ok {
			v = capWebText(s, maxWebText)
		}
		out[k] = v
	}
	return out
}

// capWebText cuts s to at most n bytes on a rune boundary, noting the cut.
func capWebText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + fmt.Sprintf("\n[... %d more bytes; full text: suag export]", len(s)-cut)
}

// joinEntries links entries to events: a tool event gets its call and
// result (by call id), a model event the assistant messages written after
// it started and before the next model call of the turn.
func joinEntries(view *TraceView) {
	byCall := map[string][]int64{}
	for _, en := range view.Entries {
		if en.CallID != "" {
			byCall[en.CallID] = append(byCall[en.CallID], en.Seq)
		}
	}
	var models []int // indexes of main model events, in time order
	for i := range view.Events {
		e := &view.Events[i]
		switch e.Kind {
		case store.EventTool, store.EventDenial:
			if id, _ := e.Attrs["call_id"].(string); id != "" {
				e.Entries = byCall[id]
			}
		case store.EventModel:
			if p, _ := e.Attrs["purpose"].(string); p == "" {
				models = append(models, i)
			}
		}
	}
	sort.SliceStable(models, func(a, b int) bool { return view.Events[models[a]].Start.Before(view.Events[models[b]].Start) })
	for k, i := range models {
		e := &view.Events[i]
		var until time.Time
		if k+1 < len(models) {
			until = view.Events[models[k+1]].Start
		}
		for _, en := range view.Entries {
			if en.Type != store.EntryAssistant || en.TurnID != e.TurnID || en.Time.Before(e.Start) {
				continue
			}
			if !until.IsZero() && !en.Time.Before(until) {
				continue
			}
			e.Entries = append(e.Entries, en.Seq)
		}
	}
}

// ServeTraceView serves the trace page for view on addr (a loopback
// address; ":0" picks a port) until ctx ends. env, when set, serves
// /api/env: this machine's current environment for the session (the 本机环境
// tab). ready receives the URL once the listener is up.
func ServeTraceView(ctx context.Context, addr string, load func(context.Context) (*TraceView, error),
	env func(context.Context) (any, error), ready func(url string)) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("trace --web address %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("trace --web listens on a loopback address only, got %q", addr)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(traceWebHTML)
	})
	mux.HandleFunc("/api/session", func(w http.ResponseWriter, r *http.Request) {
		// Loaded per request, so a reload shows a session still running.
		view, err := load(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(view)
	})
	mux.HandleFunc("/api/env", func(w http.ResponseWriter, r *http.Request) {
		if env == nil {
			http.NotFound(w, r)
			return
		}
		data, err := env(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data)
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	if ready != nil {
		ready("http://" + ln.Addr().String() + "/")
	}
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
