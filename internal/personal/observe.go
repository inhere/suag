package personal

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/internal/platform/llm"
	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/pkg/agentcore"
)

// Event statuses written to the events table.
const (
	eventOK     = "ok"
	eventError  = "error"
	eventDenied = "denied"
)

// maxEventError caps the error text kept on one event row.
const maxEventError = 2000

// eventSink is the slice of *store.Store the observer needs. It is an
// interface so tests can inject write failures.
type eventSink interface {
	AppendEvent(ctx context.Context, e store.Event) (store.Event, error)
}

type scopeKey struct{}

// observeScope identifies the turn whose work is being observed. It travels in
// ctx so provider and tool wrappers (which only see ctx) can attribute events.
type observeScope struct{ sessionID, turnID string }

func withObserveScope(ctx context.Context, sessionID, turnID string) context.Context {
	return context.WithValue(ctx, scopeKey{}, observeScope{sessionID: sessionID, turnID: turnID})
}

func observeScopeFrom(ctx context.Context) observeScope {
	s, _ := ctx.Value(scopeKey{}).(observeScope)
	return s
}

// recorder writes observability events best-effort: a failed write is logged
// and never affects the turn. Events are written outside any entry transaction.
type recorder struct {
	sink eventSink
}

func (r *recorder) record(ctx context.Context, e store.Event) {
	if r == nil || r.sink == nil {
		return
	}
	scope := observeScopeFrom(ctx)
	if e.SessionID == "" {
		e.SessionID = scope.sessionID
	}
	if e.TurnID == "" {
		e.TurnID = scope.turnID
	}
	if e.SessionID == "" {
		return // not inside a turn; nothing to attribute the event to
	}
	if len(e.Error) > maxEventError {
		e.Error = e.Error[:maxEventError]
	}
	// A cancelled turn must still persist its events.
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err := r.sink.AppendEvent(wctx, e); err != nil {
		slog.Warn("record observability event", "kind", e.Kind, "name", e.Name, "session_id", e.SessionID, "err", err)
	}
}

// attemptObserver turns a provider attempt into a model event. defModel and
// defName fill in what the provider itself does not report.
func (r *recorder) attemptObserver(defName, defModel string) llm.AttemptObserver {
	return func(ctx context.Context, a llm.Attempt) {
		name, model := a.Provider, a.Model
		if defName != "" && (name == "" || name == "pool") {
			name = defName
		}
		if model == "" {
			model = defModel
		}
		e := store.Event{
			Kind: store.EventModel, Name: name, Status: eventOK,
			StartedAt: a.StartedAt, DurationMs: a.Duration.Milliseconds(), TTFTMs: ttftMillis(a.TTFT),
			// Provider-normalized: InputTokens includes cache (see ReportUsage).
			InputTokens: int64(a.Usage.InputTokens), OutputTokens: int64(a.Usage.OutputTokens),
			CacheReadTokens: int64(a.Usage.CacheReadTokens), CacheWriteTokens: int64(a.Usage.CacheWriteTokens),
			Attrs: map[string]any{"model": model, "attempt": a.Index, "streamed": a.Streamed},
		}
		if a.Usage.CostUSD != 0 {
			e.Attrs["cost_usd"] = a.Usage.CostUSD
		}
		if a.Err != nil {
			e.Status, e.Error = eventError, a.Err.Error()
		}
		r.record(ctx, e)
	}
}

// observedProvider reports every Generate call as attempt 1.
type observedProvider struct {
	inner agentcore.Provider
	obs   llm.AttemptObserver
}

func (p *observedProvider) Name() string { return p.inner.Name() }

func (p *observedProvider) Generate(ctx context.Context, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	return llm.GenerateObserved(ctx, p.inner, req, 1, p.obs)
}

// observedStreamingProvider additionally implements Stream. It is only used
// when the inner provider streams, so the runtime's StreamingProvider type
// assertion gives the same answer with and without observation.
type observedStreamingProvider struct {
	observedProvider
	stream agentcore.StreamingProvider
}

func (p *observedStreamingProvider) Stream(ctx context.Context, req *agentcore.GenerateRequest, emit agentcore.StreamFunc) (*agentcore.GenerateResult, error) {
	return llm.StreamObserved(ctx, p.stream, req, emit, 1, p.obs)
}

// observeProvider makes provider calls emit model events. A provider that can
// report its own attempts (llm.Pool, with per-fallback attempts) gets the
// observer installed instead of being wrapped.
func (r *recorder) observeProvider(p agentcore.Provider, name, model string) agentcore.Provider {
	obs := r.attemptObserver(name, model)
	if settable, ok := p.(interface{ SetObserver(llm.AttemptObserver) }); ok {
		settable.SetObserver(obs)
		return p
	}
	base := observedProvider{inner: p, obs: obs}
	if s, ok := p.(agentcore.StreamingProvider); ok {
		return &observedStreamingProvider{observedProvider: base, stream: s}
	}
	return &base
}

// observedTools is the outermost tool-registry layer (observe -> visibility ->
// approval -> base). It records a tool event per call and a denial event when
// an inner layer refused the call with tools.ErrDenied.
type observedTools struct {
	inner agentcore.ToolRegistry
	rec   *recorder
}

func (r *recorder) observeTools(inner agentcore.ToolRegistry) agentcore.ToolRegistry {
	if inner == nil {
		return nil
	}
	return &observedTools{inner: inner, rec: r}
}

func (o *observedTools) ListTools(ctx context.Context) ([]agentcore.ToolSchema, error) {
	return o.inner.ListTools(ctx)
}

// Get passes tool lookup through to the wrapped layers.
func (o *observedTools) Get(name string) (tools.Tool, error) {
	if l, ok := o.inner.(tools.Lookup); ok {
		return l.Get(name)
	}
	return nil, errors.New("tool lookup is not supported by the wrapped registry")
}

func (o *observedTools) ExecuteTool(ctx context.Context, call agentcore.ToolCall) (*agentcore.ToolResult, error) {
	start := time.Now()
	res, err := o.inner.ExecuteTool(ctx, call)
	e := store.Event{
		Kind: store.EventTool, Name: call.Name, Status: eventOK, StartedAt: start,
		DurationMs: time.Since(start).Milliseconds(),
		Attrs:      map[string]any{"call_id": call.CallID},
	}
	switch {
	case err != nil && errors.Is(err, tools.ErrDenied):
		e.Kind, e.Status, e.Error = store.EventDenial, eventDenied, err.Error()
	case err != nil:
		e.Status, e.Error = eventError, err.Error()
	case res != nil && res.Metadata["is_error"] == true:
		e.Status, e.Error = eventError, "tool reported an error result"
	}
	o.rec.record(ctx, e)
	return res, err
}

// ttftMillis rounds a measured sub-millisecond TTFT up to 1ms so "a first delta
// was seen" stays distinguishable from "no TTFT" (0).
func ttftMillis(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return max(d.Milliseconds(), 1)
}
