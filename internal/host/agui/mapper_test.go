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
	customEvent, ok := got.(CustomEvent)
	if !ok {
		t.Fatalf("MapEvent() type = %T, want %T", got, CustomEvent{})
	}
	if customEvent.Type != EventTypeCustom {
		t.Fatalf("MapEvent().Type = %q, want %q", customEvent.Type, EventTypeCustom)
	}
	if customEvent.Name != "axon.run.queued" {
		t.Fatalf("MapEvent().Name = %q, want %q", customEvent.Name, "axon.run.queued")
	}

	value, ok := customEvent.Value.(map[string]any)
	if !ok {
		t.Fatalf("MapEvent().Value type = %T, want map[string]any", customEvent.Value)
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
			AgentID:   "default",
			Sequence:  2,
		},
		{
			Type:      "run.started",
			SessionID: "sess_001",
			TurnID:    "turn_002",
			RunID:     "run_002",
			AgentID:   "default",
			Sequence:  3,
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
	if snapshot["currentRunId"] != "run_002" {
		t.Fatalf("BuildStateSnapshot().Snapshot.currentRunId = %v, want %q", snapshot["currentRunId"], "run_002")
	}
	if snapshot["currentAgentId"] != "default" {
		t.Fatalf("BuildStateSnapshot().Snapshot.currentAgentId = %v, want %q", snapshot["currentAgentId"], "default")
	}
	if snapshot["latestEventType"] != "run.started" {
		t.Fatalf("BuildStateSnapshot().Snapshot.latestEventType = %v, want %q", snapshot["latestEventType"], "run.started")
	}
	if snapshot["eventCount"] != 2 {
		t.Fatalf("BuildStateSnapshot().Snapshot.eventCount = %v, want %d", snapshot["eventCount"], 2)
	}

	rawEvents, ok := snapshot["events"].([]map[string]any)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot.events type = %T, want []map[string]any", snapshot["events"])
	}
	if len(rawEvents) != 2 {
		t.Fatalf("BuildStateSnapshot().Snapshot.events len = %d, want 2", len(rawEvents))
	}
	if rawEvents[1]["type"] != "run.started" {
		t.Fatalf("BuildStateSnapshot().Snapshot.events[1].type = %v, want %q", rawEvents[1]["type"], "run.started")
	}
}

func TestMapEvent_RunStartedToStandardRunStarted(t *testing.T) {
	t.Parallel()

	event := HostEvent{
		Type:      "run.started",
		SessionID: "sess_001",
		TurnID:    "turn_001",
		RunID:     "run_001",
	}

	got := MapEvent(event)
	runEvent, ok := got.(RunStartedEvent)
	if !ok {
		t.Fatalf("MapEvent() type = %T, want %T", got, RunStartedEvent{})
	}
	if runEvent.Type != EventTypeRunStarted {
		t.Fatalf("MapEvent().Type = %q, want %q", runEvent.Type, EventTypeRunStarted)
	}
	if runEvent.ThreadID != "sess_001" {
		t.Fatalf("MapEvent().ThreadID = %q, want %q", runEvent.ThreadID, "sess_001")
	}
	if runEvent.RunID != "run_001" {
		t.Fatalf("MapEvent().RunID = %q, want %q", runEvent.RunID, "run_001")
	}
}

func TestMapEvent_RunFinishedToStandardRunFinished(t *testing.T) {
	t.Parallel()

	event := HostEvent{
		Type:      "run.finished",
		SessionID: "sess_001",
		TurnID:    "turn_001",
		RunID:     "run_001",
		Payload: map[string]any{
			"summary": "done",
		},
	}

	got := MapEvent(event)
	runEvent, ok := got.(RunFinishedEvent)
	if !ok {
		t.Fatalf("MapEvent() type = %T, want %T", got, RunFinishedEvent{})
	}
	if runEvent.Type != EventTypeRunFinished {
		t.Fatalf("MapEvent().Type = %q, want %q", runEvent.Type, EventTypeRunFinished)
	}
	if runEvent.ThreadID != "sess_001" {
		t.Fatalf("MapEvent().ThreadID = %q, want %q", runEvent.ThreadID, "sess_001")
	}
	if runEvent.RunID != "run_001" {
		t.Fatalf("MapEvent().RunID = %q, want %q", runEvent.RunID, "run_001")
	}
}

func TestMapEvent_RunFailedToStandardRunError(t *testing.T) {
	t.Parallel()

	event := HostEvent{
		Type:      "run.failed",
		SessionID: "sess_001",
		TurnID:    "turn_001",
		RunID:     "run_001",
		Payload: map[string]any{
			"error": "runner failed",
		},
	}

	got := MapEvent(event)
	runEvent, ok := got.(RunErrorEvent)
	if !ok {
		t.Fatalf("MapEvent() type = %T, want %T", got, RunErrorEvent{})
	}
	if runEvent.Type != EventTypeRunError {
		t.Fatalf("MapEvent().Type = %q, want %q", runEvent.Type, EventTypeRunError)
	}
	if runEvent.RunID != "run_001" {
		t.Fatalf("MapEvent().RunID = %q, want %q", runEvent.RunID, "run_001")
	}
	if runEvent.Message != "runner failed" {
		t.Fatalf("MapEvent().Message = %q, want %q", runEvent.Message, "runner failed")
	}
}
