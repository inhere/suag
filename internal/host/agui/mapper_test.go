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
	if len(got) != 2 {
		t.Fatalf("MapEvent() len = %d, want 2", len(got))
	}
	customEvent, ok := got[0].(CustomEvent)
	if !ok {
		t.Fatalf("MapEvent()[0] type = %T, want %T", got[0], CustomEvent{})
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

	activityEvent, ok := got[1].(ActivitySnapshotEvent)
	if !ok {
		t.Fatalf("MapEvent()[1] type = %T, want %T", got[1], ActivitySnapshotEvent{})
	}
	if activityEvent.ActivityType != "run_status" {
		t.Fatalf("MapEvent().ActivityType = %q, want %q", activityEvent.ActivityType, "run_status")
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
	if len(got) != 2 {
		t.Fatalf("MapEvent() len = %d, want 2", len(got))
	}

	runEvent, ok := got[0].(RunStartedEvent)
	if !ok {
		t.Fatalf("MapEvent()[0] type = %T, want %T", got[0], RunStartedEvent{})
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

	activityEvent, ok := got[1].(ActivitySnapshotEvent)
	if !ok {
		t.Fatalf("MapEvent()[1] type = %T, want %T", got[1], ActivitySnapshotEvent{})
	}
	if activityEvent.ActivityType != "run_status" {
		t.Fatalf("MapEvent().ActivityType = %q, want %q", activityEvent.ActivityType, "run_status")
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
	if len(got) != 5 {
		t.Fatalf("MapEvent() len = %d, want 5", len(got))
	}

	runEvent, ok := got[0].(RunFinishedEvent)
	if !ok {
		t.Fatalf("MapEvent()[0] type = %T, want %T", got[0], RunFinishedEvent{})
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

	activityEvent, ok := got[1].(ActivitySnapshotEvent)
	if !ok {
		t.Fatalf("MapEvent()[1] type = %T, want %T", got[1], ActivitySnapshotEvent{})
	}
	content, ok := activityEvent.Content.(map[string]any)
	if !ok {
		t.Fatalf("MapEvent().Content type = %T, want map[string]any", activityEvent.Content)
	}
	if content["status"] != "completed" {
		t.Fatalf("MapEvent().Content.status = %v, want %q", content["status"], "completed")
	}

	startEvent, ok := got[2].(TextMessageStartEvent)
	if !ok {
		t.Fatalf("MapEvent()[2] type = %T, want %T", got[2], TextMessageStartEvent{})
	}
	if startEvent.Role != "assistant" {
		t.Fatalf("MapEvent().Role = %q, want %q", startEvent.Role, "assistant")
	}

	contentEvent, ok := got[3].(TextMessageContentEvent)
	if !ok {
		t.Fatalf("MapEvent()[3] type = %T, want %T", got[3], TextMessageContentEvent{})
	}
	if contentEvent.Delta != "done" {
		t.Fatalf("MapEvent().Delta = %q, want %q", contentEvent.Delta, "done")
	}

	endEvent, ok := got[4].(TextMessageEndEvent)
	if !ok {
		t.Fatalf("MapEvent()[4] type = %T, want %T", got[4], TextMessageEndEvent{})
	}
	if endEvent.MessageID != startEvent.MessageID {
		t.Fatalf("MapEvent().End.MessageID = %q, want %q", endEvent.MessageID, startEvent.MessageID)
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
	if len(got) != 2 {
		t.Fatalf("MapEvent() len = %d, want 2", len(got))
	}

	runEvent, ok := got[0].(RunErrorEvent)
	if !ok {
		t.Fatalf("MapEvent()[0] type = %T, want %T", got[0], RunErrorEvent{})
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

	activityEvent, ok := got[1].(ActivitySnapshotEvent)
	if !ok {
		t.Fatalf("MapEvent()[1] type = %T, want %T", got[1], ActivitySnapshotEvent{})
	}
	content, ok := activityEvent.Content.(map[string]any)
	if !ok {
		t.Fatalf("MapEvent().Content type = %T, want map[string]any", activityEvent.Content)
	}
	if content["status"] != "failed" {
		t.Fatalf("MapEvent().Content.status = %v, want %q", content["status"], "failed")
	}
}

func TestMapEvent_RunQueuedIncludesActivitySnapshot(t *testing.T) {
	t.Parallel()

	event := HostEvent{
		Type:      "run.queued",
		SessionID: "sess_001",
		TurnID:    "turn_001",
		RunID:     "run_001",
		AgentID:   "default",
		Sequence:  1,
	}

	got := MapEvent(event)
	if len(got) != 2 {
		t.Fatalf("MapEvent() len = %d, want 2", len(got))
	}
	activityEvent, ok := got[1].(ActivitySnapshotEvent)
	if !ok {
		t.Fatalf("MapEvent()[1] type = %T, want %T", got[1], ActivitySnapshotEvent{})
	}
	content, ok := activityEvent.Content.(map[string]any)
	if !ok {
		t.Fatalf("MapEvent().Content type = %T, want map[string]any", activityEvent.Content)
	}
	if content["status"] != "queued" {
		t.Fatalf("MapEvent().Content.status = %v, want %q", content["status"], "queued")
	}
}
