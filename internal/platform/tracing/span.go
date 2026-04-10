// Package tracing provides full-chain tracing with TraceID/SpanID for agent operations.
package tracing

import (
	"fmt"
	"math/rand"
	"time"
)

// Span records a single operation within a trace.
type Span struct {
	TraceID   string
	SpanID    string
	ParentID  string // parent SpanID; empty for root spans
	AgentID   string
	Operation string // "llm_call" | "tool_call" | "hitl_wait" | "task" etc.
	Input     string
	Output    string
	StartTime time.Time
	EndTime   time.Time
	Status    string // "ok" | "error"
	Error     string
	Meta      map[string]string // arbitrary key-value metadata
}

// Duration returns the elapsed time for this span.
func (s *Span) Duration() time.Duration {
	if s.EndTime.IsZero() {
		return time.Since(s.StartTime)
	}
	return s.EndTime.Sub(s.StartTime)
}

// IsRoot returns true if this span has no parent.
func (s *Span) IsRoot() bool { return s.ParentID == "" }

// Finish marks the span as complete with the given error (nil = success).
func (s *Span) Finish(err error) {
	s.EndTime = time.Now()
	if err != nil {
		s.Status = "error"
		s.Error = err.Error()
	} else {
		s.Status = "ok"
	}
}

// NewTraceID generates a random 128-bit trace identifier.
func NewTraceID() string {
	return fmt.Sprintf("%016x%016x", rand.Int63(), rand.Int63()) //nolint:gosec
}

// NewSpanID generates a random 64-bit span identifier.
func NewSpanID() string {
	return fmt.Sprintf("%016x", rand.Int63()) //nolint:gosec
}
