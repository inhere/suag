package agentcore

import (
	"strings"
	"testing"
)

func TestGenerateResultValidate_RejectsEmptyResult(t *testing.T) {
	res := &GenerateResult{}
	err := res.Validate()
	if err == nil || !strings.Contains(err.Error(), "empty result") {
		t.Fatalf("expected empty result error, got %v", err)
	}
}

func TestGenerateResultValidate_RejectsMixedFinalAndToolCalls(t *testing.T) {
	res := &GenerateResult{
		Content:      "done",
		ToolCalls:    []ToolCall{{CallID: "call-1", Name: "echo"}},
		FinishReason: FinishToolCalls,
	}

	err := res.Validate()
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error, got %v", err)
	}
}

func TestGenerateResultValidate_RejectsToolCallWithoutID(t *testing.T) {
	res := &GenerateResult{
		ToolCalls:    []ToolCall{{Name: "echo"}},
		FinishReason: FinishToolCalls,
	}

	err := res.Validate()
	if err == nil || !strings.Contains(err.Error(), "call id") {
		t.Fatalf("expected missing call id error, got %v", err)
	}
}

func TestGenerateResultValidate_RejectsMissingFinishReason(t *testing.T) {
	res := &GenerateResult{Content: "done"}
	err := res.Validate()
	if err == nil || !strings.Contains(err.Error(), "finish reason") {
		t.Fatalf("expected missing finish reason error, got %v", err)
	}
}

func TestControl_Yield(t *testing.T) {
	ctrl := NewControl()
	ctrl.Yield("await_approval", map[string]any{"policy": "manual"})
	if !ctrl.ShouldYield() {
		t.Fatal("expected control to yield")
	}
}
