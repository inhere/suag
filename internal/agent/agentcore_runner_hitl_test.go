package agent

import (
	"testing"

	"github.com/inhere/axon/pkg/agentcore"
)

func TestHITLFailureTracker_BuildsPendingInterventionAfterThreshold(t *testing.T) {
	tracker := &hitlFailureTracker{failThresh: 2}
	call := agentcore.ToolCall{CallID: "call-1", Name: "fail_tool"}

	tracker.markFailure(call, agentcore.ToolErrorDecision{
		Result: &agentcore.ToolResult{
			CallID:   call.CallID,
			Name:     call.Name,
			Content:  "tool error: boom",
			Metadata: map[string]any{"tool_error": true},
		},
	})

	if req, ok := tracker.snapshotIntervention(); ok || req != nil {
		t.Fatalf("unexpected pending intervention before threshold: %#v, %v", req, ok)
	}

	tracker.markFailure(call, agentcore.ToolErrorDecision{
		Result: &agentcore.ToolResult{
			CallID:   call.CallID,
			Name:     call.Name,
			Content:  "tool error: boom",
			Metadata: map[string]any{"tool_error": true},
		},
	})

	req, ok := tracker.snapshotIntervention()
	if !ok || req == nil {
		t.Fatal("expected pending intervention after threshold")
	}
	if req.Call.Name != "fail_tool" {
		t.Fatalf("call name = %q, want fail_tool", req.Call.Name)
	}
	if req.FailureCount != 2 {
		t.Fatalf("failure count = %d, want 2", req.FailureCount)
	}
	if req.Reason != `tool "fail_tool" failed 2 times in a row` {
		t.Fatalf("reason = %q", req.Reason)
	}
	if req.Context != `工具 "fail_tool" 执行失败：boom。` {
		t.Fatalf("context = %q, want tool error message", req.Context)
	}
}

func TestHITLFailureTracker_ResetClearsPendingIntervention(t *testing.T) {
	tracker := &hitlFailureTracker{failThresh: 1}
	call := agentcore.ToolCall{CallID: "call-1", Name: "fail_tool"}

	tracker.markFailure(call, agentcore.ToolErrorDecision{
		Result: &agentcore.ToolResult{
			CallID:   call.CallID,
			Name:     call.Name,
			Content:  "tool error: boom",
			Metadata: map[string]any{"tool_error": true},
		},
	})

	req, ok := tracker.snapshotIntervention()
	if !ok || req == nil {
		t.Fatal("expected pending intervention before reset")
	}

	tracker.reset()

	req, ok = tracker.snapshotIntervention()
	if ok || req != nil {
		t.Fatalf("expected reset to clear pending intervention, got %#v, %v", req, ok)
	}
}

func TestHITLFailureTracker_DoesNotAccumulateAcrossDifferentTools(t *testing.T) {
	tracker := &hitlFailureTracker{failThresh: 2}

	tracker.markFailure(agentcore.ToolCall{CallID: "call-1", Name: "tool_a"}, agentcore.ToolErrorDecision{
		Result: &agentcore.ToolResult{
			CallID:   "call-1",
			Name:     "tool_a",
			Content:  "tool error: a",
			Metadata: map[string]any{"tool_error": true},
		},
	})
	tracker.markFailure(agentcore.ToolCall{CallID: "call-2", Name: "tool_b"}, agentcore.ToolErrorDecision{
		Result: &agentcore.ToolResult{
			CallID:   "call-2",
			Name:     "tool_b",
			Content:  "tool error: b",
			Metadata: map[string]any{"tool_error": true},
		},
	})

	if req, ok := tracker.snapshotIntervention(); ok || req != nil {
		t.Fatalf("different failing tools should not reach threshold together: %#v, %v", req, ok)
	}

	tracker.markFailure(agentcore.ToolCall{CallID: "call-3", Name: "tool_b"}, agentcore.ToolErrorDecision{
		Result: &agentcore.ToolResult{
			CallID:   "call-3",
			Name:     "tool_b",
			Content:  "tool error: b again",
			Metadata: map[string]any{"tool_error": true},
		},
	})

	req, ok := tracker.snapshotIntervention()
	if !ok || req == nil {
		t.Fatal("expected pending intervention after same tool failed twice")
	}
	if req.Call.Name != "tool_b" {
		t.Fatalf("call name = %q, want tool_b", req.Call.Name)
	}
	if req.FailureCount != 2 {
		t.Fatalf("failure count = %d, want 2", req.FailureCount)
	}
}

func TestNormalizeHITLFailureContext(t *testing.T) {
	t.Run("normalizes missing required argument errors", func(t *testing.T) {
		call := agentcore.ToolCall{Name: "file_read"}
		got := normalizeHITLFailureContext(call, `tool error: "path" arg is required`)
		want := `工具 "file_read" 的调用参数不完整或格式不正确："path" arg is required。`
		if got != want {
			t.Fatalf("normalizeHITLFailureContext() = %q, want %q", got, want)
		}
	})

	t.Run("normalizes invalid argument object errors", func(t *testing.T) {
		call := agentcore.ToolCall{Name: "file_read"}
		got := normalizeHITLFailureContext(call, `tool error: tool call "file_read" arguments must be a JSON object`)
		want := `工具 "file_read" 的调用参数不完整或格式不正确：tool call "file_read" arguments must be a JSON object。`
		if got != want {
			t.Fatalf("normalizeHITLFailureContext() = %q, want %q", got, want)
		}
	})

	t.Run("keeps general tool failure readable", func(t *testing.T) {
		call := agentcore.ToolCall{Name: "file_read"}
		got := normalizeHITLFailureContext(call, `tool error: permission denied`)
		want := `工具 "file_read" 执行失败：permission denied。`
		if got != want {
			t.Fatalf("normalizeHITLFailureContext() = %q, want %q", got, want)
		}
	})
}
