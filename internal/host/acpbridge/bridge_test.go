package acpbridge

import (
	"context"
	"testing"

	"github.com/inhere/axon/internal/host/session"
)

func TestRunnerAdapter_RunDispatchesTaskToBridge(t *testing.T) {
	t.Parallel()

	fakeBridge := &fakeBridge{
		result: DispatchResult{
			State:   StateCompleted,
			Summary: "external work done",
		},
	}
	adapter := NewRunnerAdapter(fakeBridge, RunnerAdapterConfig{
		AdapterName: "codex",
		Workspace:   "workspace/demo",
	})

	result, err := adapter.Run(context.Background(), session.RuntimeRequest{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "implement feature x",
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Summary != "external work done" {
		t.Fatalf("Run().Summary = %q, want %q", result.Summary, "external work done")
	}
	if fakeBridge.lastRequest.RunID != "run_001" {
		t.Fatalf("Dispatch().RunID = %q, want %q", fakeBridge.lastRequest.RunID, "run_001")
	}
	if fakeBridge.lastRequest.Workspace != "workspace/demo" {
		t.Fatalf("Dispatch().Workspace = %q, want %q", fakeBridge.lastRequest.Workspace, "workspace/demo")
	}
	if fakeBridge.lastRequest.Adapter != "codex" {
		t.Fatalf("Dispatch().Adapter = %q, want %q", fakeBridge.lastRequest.Adapter, "codex")
	}
}

func TestRunnerAdapter_RunReturnsErrorForFailedDispatch(t *testing.T) {
	t.Parallel()

	fakeBridge := &fakeBridge{
		result: DispatchResult{
			State: StateFailed,
			Error: "external failed",
		},
	}
	adapter := NewRunnerAdapter(fakeBridge, RunnerAdapterConfig{
		AdapterName: "claude-code",
		Workspace:   "workspace/demo",
	})

	_, err := adapter.Run(context.Background(), session.RuntimeRequest{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "implement broken feature",
	})
	if err == nil {
		t.Fatal("Run() error = nil, want failed dispatch error")
	}
}

type fakeBridge struct {
	lastRequest DispatchRequest
	result      DispatchResult
	err         error
}

func (f *fakeBridge) Dispatch(_ context.Context, req DispatchRequest) (DispatchResult, error) {
	f.lastRequest = req
	return f.result, f.err
}
