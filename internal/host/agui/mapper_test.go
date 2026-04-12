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
	customEvent, ok := got[0].(*CustomEvent)
	if !ok {
		t.Fatalf("MapEvent()[0] type = %T, want %T", got[0], &CustomEvent{})
	}
	if customEvent.Type() != EventTypeCustom {
		t.Fatalf("MapEvent().Type = %q, want %q", customEvent.Type(), EventTypeCustom)
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

	activityEvent, ok := got[1].(*ActivitySnapshotEvent)
	if !ok {
		t.Fatalf("MapEvent()[1] type = %T, want %T", got[1], &ActivitySnapshotEvent{})
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
		{
			Type:      session.EventRunExternalActivity,
			SessionID: "sess_001",
			TurnID:    "turn_002",
			RunID:     "run_002",
			AgentID:   "default",
			Sequence:  4,
			Payload: map[string]any{
				"externalTaskId": "ext_001",
				"kind":           "item_started",
				"itemId":         "item_001",
			},
		},
		{
			Type:      session.EventRunExternalMessageDelta,
			SessionID: "sess_001",
			TurnID:    "turn_002",
			RunID:     "run_002",
			AgentID:   "default",
			Sequence:  5,
			Payload: map[string]any{
				"externalTaskId": "ext_001",
				"messageId":      "msg_001",
				"delta":          "hello ",
				"isFirstChunk":   true,
			},
		},
		{
			Type:      session.EventRunExternalMessageDelta,
			SessionID: "sess_001",
			TurnID:    "turn_002",
			RunID:     "run_002",
			AgentID:   "default",
			Sequence:  6,
			Payload: map[string]any{
				"externalTaskId": "ext_001",
				"messageId":      "msg_001",
				"delta":          "world",
			},
		},
		{
			Type:      session.EventRunExternalMessageEnd,
			SessionID: "sess_001",
			TurnID:    "turn_002",
			RunID:     "run_002",
			AgentID:   "default",
			Sequence:  7,
			Payload: map[string]any{
				"externalTaskId": "ext_001",
				"messageId":      "msg_001",
			},
		},
	}
	externalTasks := []session.ExternalTask{
		{
			ExternalTaskID:  "ext_001",
			SessionID:       "sess_001",
			TurnID:          "turn_002",
			RunID:           "run_002",
			Adapter:         "codex",
			RemoteAgentName: "codex-cli",
			RemoteSessionID: "remote_sess_001",
			RemoteTaskID:    "remote_task_001",
			State:           "completed",
			ResultSummary:   "delegated task completed",
			Metadata: map[string]any{
				"workspace": "workspace/demo",
			},
		},
	}

	got := BuildStateSnapshot(sessionModel, events, externalTasks)
	if got.Type() != EventTypeStateSnapshot {
		t.Fatalf("BuildStateSnapshot().Type = %q, want %q", got.Type(), EventTypeStateSnapshot)
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
	if snapshot["latestEventType"] != session.EventRunExternalMessageEnd {
		t.Fatalf("BuildStateSnapshot().Snapshot.latestEventType = %v, want %q", snapshot["latestEventType"], session.EventRunExternalMessageEnd)
	}
	if snapshot["eventCount"] != 6 {
		t.Fatalf("BuildStateSnapshot().Snapshot.eventCount = %v, want %d", snapshot["eventCount"], 6)
	}
	rawExternalTasks, ok := snapshot["externalTasks"].([]map[string]any)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot.externalTasks type = %T, want []map[string]any", snapshot["externalTasks"])
	}
	if len(rawExternalTasks) != 1 {
		t.Fatalf("BuildStateSnapshot().Snapshot.externalTasks len = %d, want 1", len(rawExternalTasks))
	}
	if rawExternalTasks[0]["externalTaskId"] != "ext_001" {
		t.Fatalf("BuildStateSnapshot().Snapshot.externalTasks[0].externalTaskId = %v, want %q", rawExternalTasks[0]["externalTaskId"], "ext_001")
	}
	if rawExternalTasks[0]["remoteAgentName"] != "codex-cli" {
		t.Fatalf("BuildStateSnapshot().Snapshot.externalTasks[0].remoteAgentName = %v, want %q", rawExternalTasks[0]["remoteAgentName"], "codex-cli")
	}

	rawEvents, ok := snapshot["events"].([]map[string]any)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot.events type = %T, want []map[string]any", snapshot["events"])
	}
	if len(rawEvents) != 6 {
		t.Fatalf("BuildStateSnapshot().Snapshot.events len = %d, want 6", len(rawEvents))
	}
	if rawEvents[5]["type"] != session.EventRunExternalMessageEnd {
		t.Fatalf("BuildStateSnapshot().Snapshot.events[5].type = %v, want %q", rawEvents[5]["type"], session.EventRunExternalMessageEnd)
	}

	rawExternalActivities, ok := snapshot["externalActivities"].([]map[string]any)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot.externalActivities type = %T, want []map[string]any", snapshot["externalActivities"])
	}
	if len(rawExternalActivities) != 1 {
		t.Fatalf("BuildStateSnapshot().Snapshot.externalActivities len = %d, want 1", len(rawExternalActivities))
	}
	if rawExternalActivities[0]["externalTaskId"] != "ext_001" {
		t.Fatalf("BuildStateSnapshot().Snapshot.externalActivities[0].externalTaskId = %v, want %q", rawExternalActivities[0]["externalTaskId"], "ext_001")
	}
	if rawExternalActivities[0]["lastKind"] != "item_started" {
		t.Fatalf("BuildStateSnapshot().Snapshot.externalActivities[0].lastKind = %v, want %q", rawExternalActivities[0]["lastKind"], "item_started")
	}

	rawExternalMessages, ok := snapshot["externalMessages"].([]map[string]any)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot.externalMessages type = %T, want []map[string]any", snapshot["externalMessages"])
	}
	if len(rawExternalMessages) != 1 {
		t.Fatalf("BuildStateSnapshot().Snapshot.externalMessages len = %d, want 1", len(rawExternalMessages))
	}
	if rawExternalMessages[0]["externalTaskId"] != "ext_001" {
		t.Fatalf("BuildStateSnapshot().Snapshot.externalMessages[0].externalTaskId = %v, want %q", rawExternalMessages[0]["externalTaskId"], "ext_001")
	}
	if rawExternalMessages[0]["messageId"] != "msg_001" {
		t.Fatalf("BuildStateSnapshot().Snapshot.externalMessages[0].messageId = %v, want %q", rawExternalMessages[0]["messageId"], "msg_001")
	}
	if rawExternalMessages[0]["content"] != "hello world" {
		t.Fatalf("BuildStateSnapshot().Snapshot.externalMessages[0].content = %v, want %q", rawExternalMessages[0]["content"], "hello world")
	}
	if rawExternalMessages[0]["started"] != true {
		t.Fatalf("BuildStateSnapshot().Snapshot.externalMessages[0].started = %v, want true", rawExternalMessages[0]["started"])
	}
	if rawExternalMessages[0]["completed"] != true {
		t.Fatalf("BuildStateSnapshot().Snapshot.externalMessages[0].completed = %v, want true", rawExternalMessages[0]["completed"])
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

	runEvent, ok := got[0].(*RunStartedEvent)
	if !ok {
		t.Fatalf("MapEvent()[0] type = %T, want %T", got[0], &RunStartedEvent{})
	}
	if runEvent.Type() != EventTypeRunStarted {
		t.Fatalf("MapEvent().Type = %q, want %q", runEvent.Type(), EventTypeRunStarted)
	}
	if runEvent.ThreadID() != "sess_001" {
		t.Fatalf("MapEvent().ThreadID = %q, want %q", runEvent.ThreadID(), "sess_001")
	}
	if runEvent.RunID() != "run_001" {
		t.Fatalf("MapEvent().RunID = %q, want %q", runEvent.RunID(), "run_001")
	}

	activityEvent, ok := got[1].(*ActivitySnapshotEvent)
	if !ok {
		t.Fatalf("MapEvent()[1] type = %T, want %T", got[1], &ActivitySnapshotEvent{})
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

	runEvent, ok := got[0].(*RunFinishedEvent)
	if !ok {
		t.Fatalf("MapEvent()[0] type = %T, want %T", got[0], &RunFinishedEvent{})
	}
	if runEvent.Type() != EventTypeRunFinished {
		t.Fatalf("MapEvent().Type = %q, want %q", runEvent.Type(), EventTypeRunFinished)
	}
	if runEvent.ThreadID() != "sess_001" {
		t.Fatalf("MapEvent().ThreadID = %q, want %q", runEvent.ThreadID(), "sess_001")
	}
	if runEvent.RunID() != "run_001" {
		t.Fatalf("MapEvent().RunID = %q, want %q", runEvent.RunID(), "run_001")
	}

	activityEvent, ok := got[1].(*ActivitySnapshotEvent)
	if !ok {
		t.Fatalf("MapEvent()[1] type = %T, want %T", got[1], &ActivitySnapshotEvent{})
	}
	content, ok := activityEvent.Content.(map[string]any)
	if !ok {
		t.Fatalf("MapEvent().Content type = %T, want map[string]any", activityEvent.Content)
	}
	if content["status"] != "completed" {
		t.Fatalf("MapEvent().Content.status = %v, want %q", content["status"], "completed")
	}

	startEvent, ok := got[2].(*TextMessageStartEvent)
	if !ok {
		t.Fatalf("MapEvent()[2] type = %T, want %T", got[2], &TextMessageStartEvent{})
	}
	if startEvent.Role == nil || *startEvent.Role != "assistant" {
		t.Fatalf("MapEvent().Role = %v, want %q", startEvent.Role, "assistant")
	}

	contentEvent, ok := got[3].(*TextMessageContentEvent)
	if !ok {
		t.Fatalf("MapEvent()[3] type = %T, want %T", got[3], &TextMessageContentEvent{})
	}
	if contentEvent.Delta != "done" {
		t.Fatalf("MapEvent().Delta = %q, want %q", contentEvent.Delta, "done")
	}

	endEvent, ok := got[4].(*TextMessageEndEvent)
	if !ok {
		t.Fatalf("MapEvent()[4] type = %T, want %T", got[4], &TextMessageEndEvent{})
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

	runEvent, ok := got[0].(*RunErrorEvent)
	if !ok {
		t.Fatalf("MapEvent()[0] type = %T, want %T", got[0], &RunErrorEvent{})
	}
	if runEvent.Type() != EventTypeRunError {
		t.Fatalf("MapEvent().Type = %q, want %q", runEvent.Type(), EventTypeRunError)
	}
	if runEvent.RunID() != "run_001" {
		t.Fatalf("MapEvent().RunID = %q, want %q", runEvent.RunID(), "run_001")
	}
	if runEvent.Message != "runner failed" {
		t.Fatalf("MapEvent().Message = %q, want %q", runEvent.Message, "runner failed")
	}

	activityEvent, ok := got[1].(*ActivitySnapshotEvent)
	if !ok {
		t.Fatalf("MapEvent()[1] type = %T, want %T", got[1], &ActivitySnapshotEvent{})
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
	activityEvent, ok := got[1].(*ActivitySnapshotEvent)
	if !ok {
		t.Fatalf("MapEvent()[1] type = %T, want %T", got[1], &ActivitySnapshotEvent{})
	}
	content, ok := activityEvent.Content.(map[string]any)
	if !ok {
		t.Fatalf("MapEvent().Content type = %T, want map[string]any", activityEvent.Content)
	}
	if content["status"] != "queued" {
		t.Fatalf("MapEvent().Content.status = %v, want %q", content["status"], "queued")
	}
}

func TestMapEvent_RunExternalActivityMapsToCustomAndSnapshot(t *testing.T) {
	t.Parallel()

	event := HostEvent{
		Type:      "run.external.activity",
		SessionID: "sess_001",
		TurnID:    "turn_001",
		RunID:     "run_001",
		AgentID:   "default",
		Sequence:  4,
		Payload: map[string]any{
			"kind":   "item_started",
			"itemId": "item_001",
		},
	}

	got := MapEvent(event)
	if len(got) != 2 {
		t.Fatalf("MapEvent() len = %d, want 2", len(got))
	}
	customEvent, ok := got[0].(*CustomEvent)
	if !ok {
		t.Fatalf("MapEvent()[0] type = %T, want %T", got[0], &CustomEvent{})
	}
	if customEvent.Name != "axon.run.external.activity" {
		t.Fatalf("MapEvent()[0].Name = %q, want %q", customEvent.Name, "axon.run.external.activity")
	}
	activityEvent, ok := got[1].(*ActivitySnapshotEvent)
	if !ok {
		t.Fatalf("MapEvent()[1] type = %T, want %T", got[1], &ActivitySnapshotEvent{})
	}
	if activityEvent.ActivityType != "external_activity" {
		t.Fatalf("MapEvent()[1].ActivityType = %q, want %q", activityEvent.ActivityType, "external_activity")
	}
}

func TestMapEvent_RunExternalMessageDeltaMapsToTextEvents(t *testing.T) {
	t.Parallel()

	event := HostEvent{
		Type:      "run.external.message.delta",
		SessionID: "sess_001",
		TurnID:    "turn_001",
		RunID:     "run_001",
		Payload: map[string]any{
			"messageId":    "turn_remote_001:item_001",
			"delta":        "hello",
			"isFirstChunk": true,
		},
	}

	got := MapEvent(event)
	if len(got) != 2 {
		t.Fatalf("MapEvent() len = %d, want 2", len(got))
	}
	if _, ok := got[0].(*TextMessageStartEvent); !ok {
		t.Fatalf("MapEvent()[0] type = %T, want %T", got[0], &TextMessageStartEvent{})
	}
	contentEvent, ok := got[1].(*TextMessageContentEvent)
	if !ok {
		t.Fatalf("MapEvent()[1] type = %T, want %T", got[1], &TextMessageContentEvent{})
	}
	if contentEvent.Delta != "hello" {
		t.Fatalf("MapEvent()[1].Delta = %q, want %q", contentEvent.Delta, "hello")
	}
}

func TestMapEvent_RunExternalMessageEndMapsToTextEnd(t *testing.T) {
	t.Parallel()

	event := HostEvent{
		Type:      "run.external.message.end",
		SessionID: "sess_001",
		TurnID:    "turn_001",
		RunID:     "run_001",
		Payload: map[string]any{
			"messageId": "turn_remote_001:item_001",
		},
	}

	got := MapEvent(event)
	if len(got) != 1 {
		t.Fatalf("MapEvent() len = %d, want 1", len(got))
	}
	endEvent, ok := got[0].(*TextMessageEndEvent)
	if !ok {
		t.Fatalf("MapEvent()[0] type = %T, want %T", got[0], &TextMessageEndEvent{})
	}
	if endEvent.MessageID != "turn_remote_001:item_001" {
		t.Fatalf("MapEvent()[0].MessageID = %q, want %q", endEvent.MessageID, "turn_remote_001:item_001")
	}
}
