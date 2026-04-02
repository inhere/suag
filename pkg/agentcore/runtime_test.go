package agentcore

import (
	"errors"
	"testing"
)

func TestGenerateResultValidate_RejectsEmptyResult(t *testing.T) {
	res := &GenerateResult{}
	err := res.Validate()
	if !errors.Is(err, ErrEmptyGenerateResult) {
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
	if !errors.Is(err, ErrMixedGenerateResult) {
		t.Fatalf("expected mutually exclusive error, got %v", err)
	}
}

func TestGenerateResultValidate_RejectsToolCallWithoutID(t *testing.T) {
	res := &GenerateResult{
		ToolCalls:    []ToolCall{{Name: "echo"}},
		FinishReason: FinishToolCalls,
	}

	err := res.Validate()
	if !errors.Is(err, ErrMissingToolCallID) {
		t.Fatalf("expected missing call id error, got %v", err)
	}
}

func TestGenerateResultValidate_RejectsMissingFinishReason(t *testing.T) {
	res := &GenerateResult{Content: "done"}
	err := res.Validate()
	if !errors.Is(err, ErrMissingFinishReason) {
		t.Fatalf("expected missing finish reason error, got %v", err)
	}
}

func TestGenerateResultValidate_RejectsFinalContentWithToolFinishReason(t *testing.T) {
	res := &GenerateResult{
		Content:      "done",
		FinishReason: FinishToolCalls,
	}

	err := res.Validate()
	if !errors.Is(err, ErrInvalidFinishReason) {
		t.Fatalf("expected invalid finish reason error, got %v", err)
	}
}

func TestGenerateResultValidate_RejectsToolCallsWithFinalFinishReason(t *testing.T) {
	res := &GenerateResult{
		ToolCalls:    []ToolCall{{CallID: "call-1", Name: "echo"}},
		FinishReason: FinishFinal,
	}

	err := res.Validate()
	if !errors.Is(err, ErrInvalidFinishReason) {
		t.Fatalf("expected invalid finish reason error, got %v", err)
	}
}

func TestControl_Yield(t *testing.T) {
	ctrl := NewControl()
	ctrl.Yield("await_approval", map[string]any{"policy": "manual"})
	if !ctrl.ShouldYield() {
		t.Fatal("expected control to yield")
	}
}

func TestControl_YieldCopiesMetadata(t *testing.T) {
	metadata := map[string]any{"policy": "manual"}
	ctrl := NewControl()
	ctrl.Yield("await_approval", metadata)

	metadata["policy"] = "auto"

	if got := ctrl.YieldMetadata["policy"]; got != "manual" {
		t.Fatalf("expected copied metadata to stay manual, got %v", got)
	}
}

func TestControl_ShortCircuit(t *testing.T) {
	ctrl := NewControl()
	ctrl.ShortCircuit("replacement")

	if !ctrl.ShouldShortCircuit() {
		t.Fatal("expected control to short circuit")
	}
	if ctrl.Replacement != "replacement" {
		t.Fatalf("expected replacement to be preserved, got %v", ctrl.Replacement)
	}
}
