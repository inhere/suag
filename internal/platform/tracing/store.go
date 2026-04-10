package tracing

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// InMemoryTracer is a thread-safe in-process Tracer implementation.
// Spans are stored in memory — suitable for Phase 1 and testing.
type InMemoryTracer struct {
	mu     sync.RWMutex
	spans  map[string][]*Span // keyed by traceID
	notify func(*Span)        // optional hook for SSE broadcast
}

// NewInMemoryTracer creates an InMemoryTracer. Pass a notify func to receive
// span completion events (e.g. for SSE push); pass nil to disable.
func NewInMemoryTracer(notify func(*Span)) *InMemoryTracer {
	return &InMemoryTracer{
		spans:  make(map[string][]*Span),
		notify: notify,
	}
}

// StartSpan implements Tracer.
func (t *InMemoryTracer) StartSpan(ctx context.Context, agentID, operation string) (context.Context, *Span) {
	tc := FromContext(ctx)

	spanID := NewSpanID()
	traceID := tc.TraceID
	if traceID == "" {
		traceID = NewTraceID()
	}

	span := &Span{
		TraceID:   traceID,
		SpanID:    spanID,
		ParentID:  tc.SpanID,
		AgentID:   agentID,
		Operation: operation,
		StartTime: time.Now(),
		Meta:      make(map[string]string),
	}

	t.mu.Lock()
	t.spans[traceID] = append(t.spans[traceID], span)
	t.mu.Unlock()

	newTC := TraceContext{TraceID: traceID, SpanID: spanID, ParentID: tc.SpanID}
	return WithTrace(ctx, newTC), span
}

// FinishSpan implements Tracer.
func (t *InMemoryTracer) FinishSpan(span *Span, err error) {
	span.Finish(err)
	if t.notify != nil {
		t.notify(span)
	}
}

// GetTrace implements Tracer.
func (t *InMemoryTracer) GetTrace(traceID string) ([]*Span, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	spans, ok := t.spans[traceID]
	if !ok {
		return nil, fmt.Errorf("trace %q not found", traceID)
	}
	// Return a copy to avoid external mutation.
	out := make([]*Span, len(spans))
	copy(out, spans)
	return out, nil
}

// AllTraces returns all known trace IDs.
func (t *InMemoryTracer) AllTraces() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	ids := make([]string, 0, len(t.spans))
	for id := range t.spans {
		ids = append(ids, id)
	}
	return ids
}
