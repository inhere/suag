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

func TestGenerateResultValidate_AllowsTextWithToolCalls(t *testing.T) {
	res := &GenerateResult{
		Content:      "done",
		ToolCalls:    []ToolCall{{CallID: "call-1", Name: "echo"}},
		FinishReason: FinishToolCalls,
	}

	if err := res.Validate(); err != nil {
		t.Fatalf("text alongside tool calls must be valid, got %v", err)
	}
	res.FinishReason = FinishFinal
	if err := res.Validate(); !errors.Is(err, ErrInvalidFinishReason) {
		t.Fatalf("tool calls with final finish reason must be rejected, got %v", err)
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

func TestGenerateResultValidate_RejectsToolCallWithoutName(t *testing.T) {
	res := &GenerateResult{
		ToolCalls:    []ToolCall{{CallID: "call-1"}},
		FinishReason: FinishToolCalls,
	}

	err := res.Validate()
	if !errors.Is(err, ErrMissingToolName) {
		t.Fatalf("expected missing tool name error, got %v", err)
	}
}

func TestGenerateResultValidate_RejectsMissingFinishReason(t *testing.T) {
	res := &GenerateResult{Content: "done"}
	err := res.Validate()
	if !errors.Is(err, ErrMissingFinishReason) {
		t.Fatalf("expected missing finish reason error, got %v", err)
	}
}

func TestDefaultStorage_UsesMemoryStorage(t *testing.T) {
	if _, ok := defaultStorage(nil).(*MemoryStorage); !ok {
		t.Fatalf("expected default storage to be *MemoryStorage, got %T", defaultStorage(nil))
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

func TestRuntimeErrorCategories_ArePublicWrappers(t *testing.T) {
	base := errors.New("boom")

	errs := []error{
		&ProviderError{Err: base},
		&ToolError{Err: base},
		&HookError{Err: base},
		&StorageError{Err: base},
	}

	if len(errs) != 4 {
		t.Fatal("expected public runtime error categories to be instantiable")
	}

	var providerErr *ProviderError
	if !errors.As(errs[0], &providerErr) || !errors.Is(errs[0], base) {
		t.Fatalf("expected provider error wrapper semantics, got %v", errs[0])
	}
}

func TestRunStateAndRunResult_ArePublicTypes(t *testing.T) {
	state := RunState{
		RunID:     "run-1",
		AgentID:   "agent-1",
		Status:    RunRunning,
		Step:      2,
		Messages:  []Message{{Role: "user", Content: "hello"}},
		Artifacts: []Artifact{{Kind: "file", URI: "out.txt"}},
		Metadata:  map[string]any{"scope": "contract"},
		Scratch:   map[string]any{"planner": "draft"},
		Usage:     Usage{TotalTokens: 5},
		LastError: "previous failure",
	}
	result := RunResult{
		RunID:  "run-1",
		Status: RunYielded,
		Output: "paused",
		Steps:  2,
		Usage:  Usage{TotalTokens: 5},
		Yield:  &Yield{Reason: "await_approval", ResumeOK: true},
	}

	if state.RunID == "" || result.RunID == "" {
		t.Fatal("expected public run contracts to be instantiable")
	}
}

func TestYield_IncludesResumeOK(t *testing.T) {
	yield := Yield{
		Reason:   "await_approval",
		ResumeOK: true,
	}

	if !yield.ResumeOK {
		t.Fatal("expected yield resume flag to be publicly available")
	}
}

func TestStorageContract_IncludesMessagePersistenceMethods(t *testing.T) {
	var _ Storage = (*testStorage)(nil)
}

func TestPromptAndContextBuilderContracts_UseRunState(t *testing.T) {
	var _ PromptBuilder = (*testPromptBuilder)(nil)
	var _ ContextBuilder = (*testContextBuilder)(nil)
}
