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

func TestService_StartTurnWithACPBridgePersistsExternalTaskMetadata(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newIntegrationStore(t)
	bus := newIntegrationPublisher()
	runner := acpbridge.NewRunnerAdapter(&serviceMetadataBridge{}, acpbridge.RunnerAdapterConfig{
		AdapterName: "codex",
		Workspace:   "workspace/demo",
	})

	now := time.Date(2026, 4, 12, 9, 0, 0, 0, time.UTC)
	svc := session.NewService(store, bus, runner, session.ServiceConfig{
		Now: func() time.Time { return now },
	})

	result, err := svc.StartTurn(ctx, session.StartTurnInput{
		SessionID:     "sess_001",
		AgentID:       "default",
		UserMessage:   "delegate with metadata",
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
	if tasks[0].Metadata["protocol"] != "codex_app_server" {
		t.Fatalf("ListExternalTasksByRun()[0].Metadata[protocol] = %v, want %q", tasks[0].Metadata["protocol"], "codex_app_server")
	}
	if tasks[0].Metadata["workspace"] != "workspace/demo" {
		t.Fatalf("ListExternalTasksByRun()[0].Metadata[workspace] = %v, want %q", tasks[0].Metadata["workspace"], "workspace/demo")
	}
	eventSummary, ok := tasks[0].Metadata["eventSummary"].(map[string]any)
	if !ok {
		t.Fatalf("ListExternalTasksByRun()[0].Metadata[eventSummary] type = %T, want map[string]any", tasks[0].Metadata["eventSummary"])
	}
	if !isNumericValue(eventSummary["messageDeltaCount"], 2) {
		t.Fatalf("ListExternalTasksByRun()[0].Metadata[eventSummary][messageDeltaCount] = %v, want %d", eventSummary["messageDeltaCount"], 2)
	}
}

func TestService_StartTurnWithACPBridgeRunningDispatchDoesNotFinalizeRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newIntegrationStore(t)
	bus := newIntegrationPublisher()
	runner := acpbridge.NewRunnerAdapter(&serviceRunningBridge{}, acpbridge.RunnerAdapterConfig{
		AdapterName: "codex",
		Workspace:   "workspace/demo",
	})

	now := time.Date(2026, 4, 12, 9, 30, 0, 0, time.UTC)
	svc := session.NewService(store, bus, runner, session.ServiceConfig{
		Now: func() time.Time { return now },
	})

	result, err := svc.StartTurn(ctx, session.StartTurnInput{
		SessionID:     "sess_running",
		AgentID:       "default",
		UserMessage:   "delegate and keep following",
		InitiatorType: session.InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	runModel, err := store.GetRun(ctx, result.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if runModel.State != session.RunExternal {
		t.Fatalf("persisted run state = %q, want %q", runModel.State, session.RunExternal)
	}
	if runModel.Summary != "" {
		t.Fatalf("persisted run summary = %q, want empty", runModel.Summary)
	}
	if !runModel.EndedAt.IsZero() {
		t.Fatalf("persisted run ended_at = %q, want zero", runModel.EndedAt)
	}

	events, err := store.ListEventsBySession(ctx, "sess_running", 0)
	if err != nil {
		t.Fatalf("ListEventsBySession() error = %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("ListEventsBySession() len = %d, want 4", len(events))
	}
	if events[2].EventType != session.EventRunStatus {
		t.Fatalf("events[2].EventType = %q, want %q", events[2].EventType, session.EventRunStatus)
	}
	if events[2].Payload["state"] != string(session.RunDispatching) {
		t.Fatalf("events[2].Payload[state] = %v, want %q", events[2].Payload["state"], session.RunDispatching)
	}
	if events[3].EventType != session.EventRunStatus {
		t.Fatalf("events[3].EventType = %q, want %q", events[3].EventType, session.EventRunStatus)
	}
	if events[3].Payload["state"] != string(session.RunExternal) {
		t.Fatalf("events[3].Payload[state] = %v, want %q", events[3].Payload["state"], session.RunExternal)
	}
}

type serviceMetadataBridge struct{}

func (b *serviceMetadataBridge) Dispatch(_ context.Context, _ acpbridge.DispatchRequest) (acpbridge.DispatchResult, error) {
	return acpbridge.DispatchResult{
		State:           acpbridge.StateCompleted,
		Summary:         "done",
		RemoteAgentName: "codex-cli",
		RemoteSessionID: "thread_001",
		RemoteTaskID:    "turn_remote_001",
		LastHeartbeatAt: time.Date(2026, 4, 12, 9, 0, 0, 0, time.UTC),
		Metadata: map[string]any{
			"protocol": "codex_app_server",
			"eventSummary": map[string]any{
				"messageDeltaCount": 2,
				"commandDeltaCount": 1,
			},
			"items": []any{
				map[string]any{
					"itemId":        "item_cmd_001",
					"type":          "commandExecution",
					"command":       "go test ./...",
					"outputPreview": "ok github.com/inhere/axon/internal/host/acpbridge",
				},
			},
		},
	}, nil
}

type serviceRunningBridge struct{}

func (b *serviceRunningBridge) Dispatch(_ context.Context, _ acpbridge.DispatchRequest) (acpbridge.DispatchResult, error) {
	return acpbridge.DispatchResult{
		State:           acpbridge.StateRunning,
		Summary:         "still running remotely",
		RemoteAgentName: "codex-cli",
		RemoteSessionID: "thread_001",
		RemoteTaskID:    "turn_remote_001",
		LastHeartbeatAt: time.Date(2026, 4, 12, 9, 30, 0, 0, time.UTC),
	}, nil
}

func isNumericValue(value any, want int) bool {
	switch raw := value.(type) {
	case int:
		return raw == want
	case int64:
		return raw == int64(want)
	case float64:
		return raw == float64(want)
	default:
		return false
	}
}
