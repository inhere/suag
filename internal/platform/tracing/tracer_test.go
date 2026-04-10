package tracing

import (
	"context"
	"testing"
)

func TestInMemoryTracer_StartFinish(t *testing.T) {
	tracer := NewInMemoryTracer(nil)

	ctx := context.Background()
	ctx, span := tracer.StartSpan(ctx, "agent-1", "llm_call")

	if span.TraceID == "" {
		t.Error("TraceID should be generated")
	}
	if span.SpanID == "" {
		t.Error("SpanID should be generated")
	}
	if span.Operation != "llm_call" {
		t.Errorf("Operation = %q, want llm_call", span.Operation)
	}

	tracer.FinishSpan(span, nil)
	if span.Status != "ok" {
		t.Errorf("Status = %q, want ok", span.Status)
	}
	if span.EndTime.IsZero() {
		t.Error("EndTime should be set after Finish")
	}

	// Child span should inherit TraceID.
	_, child := tracer.StartSpan(ctx, "agent-1", "tool_call")
	if child.TraceID != span.TraceID {
		t.Errorf("child TraceID %q != parent TraceID %q", child.TraceID, span.TraceID)
	}
	if child.ParentID != span.SpanID {
		t.Errorf("child ParentID %q != parent SpanID %q", child.ParentID, span.SpanID)
	}
}

func TestInMemoryTracer_GetTrace(t *testing.T) {
	tracer := NewInMemoryTracer(nil)
	ctx := context.Background()

	ctx, s1 := tracer.StartSpan(ctx, "a", "op1")
	tracer.FinishSpan(s1, nil)
	_, s2 := tracer.StartSpan(ctx, "a", "op2")
	tracer.FinishSpan(s2, nil)

	spans, err := tracer.GetTrace(s1.TraceID)
	if err != nil {
		t.Fatalf("GetTrace error: %v", err)
	}
	if len(spans) != 2 {
		t.Errorf("got %d spans, want 2", len(spans))
	}
}

func TestInMemoryTracer_ErrorSpan(t *testing.T) {
	tracer := NewInMemoryTracer(nil)
	ctx := context.Background()
	_, span := tracer.StartSpan(ctx, "a", "op")

	tracer.FinishSpan(span, context.Canceled)
	if span.Status != "error" {
		t.Errorf("Status = %q, want error", span.Status)
	}
	if span.Error == "" {
		t.Error("Error field should be set")
	}
}
