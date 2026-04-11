package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/inhere/axon/internal/host/acpbridge"
	"github.com/inhere/axon/internal/host/session"
)

func TestService_StartTurnWithACPBridgeStubPersistsExternalTask(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newIntegrationStore(t)
	bus := newIntegrationPublisher()
	bridge := acpbridge.NewStubBridge(acpbridge.StubBridgeConfig{
		RemoteAgentName: "codex-cli",
	})
	runner := acpbridge.NewRunnerAdapter(bridge, acpbridge.RunnerAdapterConfig{
		AdapterName: "codex",
		Workspace:   "workspace/demo",
	})

	now := time.Date(2026, 4, 11, 18, 30, 0, 0, time.UTC)
	svc := session.NewService(store, bus, runner, session.ServiceConfig{
		Now: func() time.Time { return now },
	})

	result, err := svc.StartTurn(ctx, session.StartTurnInput{
		SessionID:     "sess_001",
		AgentID:       "default",
		UserMessage:   "delegate to codex stub",
		InitiatorType: session.InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	tasks, err := store.ListExternalTasksByRun(ctx, result.RunID)
	if err != nil {
		t.Fatalf("ListExternalTasksByRun() error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("ListExternalTasksByRun() len = %d, want 1", len(tasks))
	}
	if tasks[0].Adapter != "codex" {
		t.Fatalf("ListExternalTasksByRun()[0].Adapter = %q, want %q", tasks[0].Adapter, "codex")
	}
	if tasks[0].RemoteAgentName != "codex-cli" {
		t.Fatalf("ListExternalTasksByRun()[0].RemoteAgentName = %q, want %q", tasks[0].RemoteAgentName, "codex-cli")
	}
	if tasks[0].State != string(acpbridge.StateCompleted) {
		t.Fatalf("ListExternalTasksByRun()[0].State = %q, want %q", tasks[0].State, acpbridge.StateCompleted)
	}
	if tasks[0].ResultSummary == "" {
		t.Fatal("ListExternalTasksByRun()[0].ResultSummary is empty")
	}
}
