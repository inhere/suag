package tracing

import "context"

// contextKey is an unexported type for context keys in this package.
type contextKey int

const traceContextKey contextKey = 1

// TraceContext carries trace propagation data through context.Context.
type TraceContext struct {
	TraceID  string
	SpanID   string
	ParentID string
}

// Tracer is the interface for creating and finishing spans.
type Tracer interface {
	// StartSpan creates a new child span under the trace carried in ctx.
	// Returns an updated context containing the new span's IDs.
	StartSpan(ctx context.Context, agentID, operation string) (context.Context, *Span)
	// FinishSpan marks a span complete. Errors are recorded on the span.
	FinishSpan(span *Span, err error)
	// GetTrace returns all spans for a given traceID.
	GetTrace(traceID string) ([]*Span, error)
}

// WithTrace injects trace context into a context.Context.
func WithTrace(ctx context.Context, tc TraceContext) context.Context {
	return context.WithValue(ctx, traceContextKey, tc)
}

// FromContext extracts the TraceContext from ctx.
// Returns zero value if none is set.
func FromContext(ctx context.Context) TraceContext {
	if tc, ok := ctx.Value(traceContextKey).(TraceContext); ok {
		return tc
	}
	return TraceContext{}
}
