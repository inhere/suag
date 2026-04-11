package acpbridge

import (
	"context"
	"testing"
)

func TestStubBridge_DispatchReturnsCompletedResult(t *testing.T) {
	t.Parallel()

	bridge := NewStubBridge(StubBridgeConfig{
		RemoteAgentName: "codex-cli",
	})

	result, err := bridge.Dispatch(context.Background(), DispatchRequest{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Adapter:     "codex",
		Instruction: "implement feature x",
		Workspace:   "workspace/demo",
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if result.State != StateCompleted {
		t.Fatalf("Dispatch().State = %q, want %q", result.State, StateCompleted)
	}
	if result.RemoteAgentName != "codex-cli" {
		t.Fatalf("Dispatch().RemoteAgentName = %q, want %q", result.RemoteAgentName, "codex-cli")
	}
	if result.RemoteSessionID == "" {
		t.Fatal("Dispatch().RemoteSessionID is empty")
	}
	if result.RemoteTaskID == "" {
		t.Fatal("Dispatch().RemoteTaskID is empty")
	}
	if result.Summary == "" {
		t.Fatal("Dispatch().Summary is empty")
	}
}

func TestStubBridge_DispatchRequiresInstruction(t *testing.T) {
	t.Parallel()

	bridge := NewStubBridge(StubBridgeConfig{})

	_, err := bridge.Dispatch(context.Background(), DispatchRequest{
		SessionID: "sess_001",
		TurnID:    "turn_001",
		RunID:     "run_001",
		AgentID:   "default",
		Adapter:   "codex",
	})
	if err == nil {
		t.Fatal("Dispatch() error = nil, want validation error")
	}
}
