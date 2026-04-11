package agui

import (
	"testing"

	"github.com/inhere/axon/internal/host/session"
)

func TestMapGatewayEvent_RunQueuedToCustom(t *testing.T) {
	t.Parallel()

	event := HostEvent{
		Type:      "run.queued",
		SessionID: "sess_001",
		TurnID:    "turn_001",
		RunID:     "run_001",
		AgentID:   "default",
		Sequence:  1,
		Payload: map[string]any{
			"message": "implement feature x",
		},
	}

	got := MapEvent(event)
	if got.Type != EventTypeCustom {
		t.Fatalf("MapEvent().Type = %q, want %q", got.Type, EventTypeCustom)
	}
	if got.Name != "axon.run.queued" {
		t.Fatalf("MapEvent().Name = %q, want %q", got.Name, "axon.run.queued")
	}

	value, ok := got.Value.(map[string]any)
	if !ok {
		t.Fatalf("MapEvent().Value type = %T, want map[string]any", got.Value)
	}
	if value["threadId"] != "sess_001" {
		t.Fatalf("MapEvent().Value.threadId = %v, want %q", value["threadId"], "sess_001")
	}
	if value["turnId"] != "turn_001" {
		t.Fatalf("MapEvent().Value.turnId = %v, want %q", value["turnId"], "turn_001")
	}
	if value["runId"] != "run_001" {
		t.Fatalf("MapEvent().Value.runId = %v, want %q", value["runId"], "run_001")
	}
}

func TestBuildStateSnapshot(t *testing.T) {
	t.Parallel()

	sessionModel := session.Session{
		SessionID:  "sess_001",
		Title:      "demo session",
		Status:     session.SessionRunning,
		LastTurnID: "turn_002",
	}
	events := []HostEvent{
		{
			Type:      "run.queued",
			SessionID: "sess_001",
			TurnID:    "turn_002",
			RunID:     "run_002",
			Sequence:  2,
		},
	}

	got := BuildStateSnapshot(sessionModel, events)
	if got.Type != EventTypeStateSnapshot {
		t.Fatalf("BuildStateSnapshot().Type = %q, want %q", got.Type, EventTypeStateSnapshot)
	}

	snapshot, ok := got.Snapshot.(map[string]any)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot type = %T, want map[string]any", got.Snapshot)
	}
	if snapshot["threadId"] != "sess_001" {
		t.Fatalf("BuildStateSnapshot().Snapshot.threadId = %v, want %q", snapshot["threadId"], "sess_001")
	}
	if snapshot["turnId"] != "turn_002" {
		t.Fatalf("BuildStateSnapshot().Snapshot.turnId = %v, want %q", snapshot["turnId"], "turn_002")
	}

	rawEvents, ok := snapshot["events"].([]map[string]any)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot.events type = %T, want []map[string]any", snapshot["events"])
	}
	if len(rawEvents) != 1 {
		t.Fatalf("BuildStateSnapshot().Snapshot.events len = %d, want 1", len(rawEvents))
	}
	if rawEvents[0]["type"] != "run.queued" {
		t.Fatalf("BuildStateSnapshot().Snapshot.events[0].type = %v, want %q", rawEvents[0]["type"], "run.queued")
	}
}
