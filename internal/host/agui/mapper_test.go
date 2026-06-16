package agui

import (
	"testing"
	"time"

	"github.com/inhere/suag/internal/host/session"
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
	if customEvent.Name != "suag.run.queued" {
		t.Fatalf("MapEvent().Name = %q, want %q", customEvent.Name, "suag.run.queued")
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

func TestMapEvent_ExternalActivitySnapshotUsesPayloadActivityID(t *testing.T) {
	t.Parallel()

	got := MapEvent(HostEvent{
		Type:      session.EventRunExternalActivity,
		SessionID: "sess_001",
		TurnID:    "turn_001",
		RunID:     "run_001",
		AgentID:   "default",
		Sequence:  7,
		Payload: map[string]any{
			"activityId": "run_001:model:2",
			"kind":       "model",
			"status":     "running",
			"title":      "LLM request",
			"detail":     "messages=3 tools=5",
		},
	})

	if len(got) != 2 {
		t.Fatalf("MapEvent() len = %d, want 2", len(got))
	}
	activityEvent, ok := got[1].(*ActivitySnapshotEvent)
	if !ok {
		t.Fatalf("MapEvent()[1] type = %T, want %T", got[1], &ActivitySnapshotEvent{})
	}
	if activityEvent.MessageID != "run_001:model:2" {
		t.Fatalf("MapEvent().MessageID = %q, want %q", activityEvent.MessageID, "run_001:model:2")
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
			CreatedAt: time.Date(2026, 4, 12, 8, 0, 0, 0, time.UTC),
			Payload: map[string]any{
				"message": "implement feature x",
			},
		},
		{
			Type:      "run.started",
			SessionID: "sess_001",
			TurnID:    "turn_002",
			RunID:     "run_002",
			AgentID:   "default",
			Sequence:  3,
			CreatedAt: time.Date(2026, 4, 12, 8, 0, 1, 0, time.UTC),
			Payload: map[string]any{
				"message": "implement feature x",
			},
		},
		{
			Type:      "run.started",
			SessionID: "sess_001",
			TurnID:    "turn_002",
			RunID:     "run_003",
			AgentID:   "reviewer",
			Sequence:  4,
			CreatedAt: time.Date(2026, 4, 12, 8, 0, 2, 0, time.UTC),
		},
		{
			Type:      session.EventRunExternalActivity,
			SessionID: "sess_001",
			TurnID:    "turn_002",
			RunID:     "run_003",
			AgentID:   "default",
			Sequence:  5,
			CreatedAt: time.Date(2026, 4, 12, 8, 0, 3, 0, time.UTC),
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
			RunID:     "run_003",
			AgentID:   "default",
			Sequence:  6,
			CreatedAt: time.Date(2026, 4, 12, 8, 0, 4, 0, time.UTC),
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
			RunID:     "run_003",
			AgentID:   "default",
			Sequence:  7,
			CreatedAt: time.Date(2026, 4, 12, 8, 0, 5, 0, time.UTC),
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
			RunID:     "run_003",
			AgentID:   "default",
			Sequence:  8,
			CreatedAt: time.Date(2026, 4, 12, 8, 0, 6, 0, time.UTC),
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
			RunID:           "run_003",
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
	if snapshot["snapshotVersion"] != StateSnapshotVersionV0 {
		t.Fatalf("BuildStateSnapshot().Snapshot.snapshotVersion = %v, want %q", snapshot["snapshotVersion"], StateSnapshotVersionV0)
	}
	if snapshot["snapshotSchema"] != StateSnapshotSchemaV0 {
		t.Fatalf("BuildStateSnapshot().Snapshot.snapshotSchema = %v, want %q", snapshot["snapshotSchema"], StateSnapshotSchemaV0)
	}
	if snapshot["generatedAt"] == "" {
		t.Fatal("BuildStateSnapshot().Snapshot.generatedAt is empty")
	}
	if snapshot["turnId"] != "turn_002" {
		t.Fatalf("BuildStateSnapshot().Snapshot.turnId = %v, want %q", snapshot["turnId"], "turn_002")
	}
	if snapshot["currentRunId"] != "run_003" {
		t.Fatalf("BuildStateSnapshot().Snapshot.currentRunId = %v, want %q", snapshot["currentRunId"], "run_003")
	}
	if snapshot["currentAgentId"] != "default" {
		t.Fatalf("BuildStateSnapshot().Snapshot.currentAgentId = %v, want %q", snapshot["currentAgentId"], "default")
	}
	if snapshot["latestEventType"] != session.EventRunExternalMessageEnd {
		t.Fatalf("BuildStateSnapshot().Snapshot.latestEventType = %v, want %q", snapshot["latestEventType"], session.EventRunExternalMessageEnd)
	}
	if snapshot["eventCount"] != 7 {
		t.Fatalf("BuildStateSnapshot().Snapshot.eventCount = %v, want %d", snapshot["eventCount"], 7)
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
	if len(rawEvents) != 7 {
		t.Fatalf("BuildStateSnapshot().Snapshot.events len = %d, want 7", len(rawEvents))
	}
	if rawEvents[6]["type"] != session.EventRunExternalMessageEnd {
		t.Fatalf("BuildStateSnapshot().Snapshot.events[6].type = %v, want %q", rawEvents[6]["type"], session.EventRunExternalMessageEnd)
	}
	if rawEvents[6]["createdAt"] != "2026-04-12T08:00:06Z" {
		t.Fatalf("BuildStateSnapshot().Snapshot.events[6].createdAt = %v, want %q", rawEvents[6]["createdAt"], "2026-04-12T08:00:06Z")
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
	if rawExternalActivities[0]["updatedAt"] != "2026-04-12T08:00:03Z" {
		t.Fatalf("BuildStateSnapshot().Snapshot.externalActivities[0].updatedAt = %v, want %q", rawExternalActivities[0]["updatedAt"], "2026-04-12T08:00:03Z")
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
	if rawExternalMessages[0]["runId"] != "run_003" {
		t.Fatalf("BuildStateSnapshot().Snapshot.externalMessages[0].runId = %v, want %q", rawExternalMessages[0]["runId"], "run_003")
	}
	if rawExternalMessages[0]["updatedAt"] != "2026-04-12T08:00:06Z" {
		t.Fatalf("BuildStateSnapshot().Snapshot.externalMessages[0].updatedAt = %v, want %q", rawExternalMessages[0]["updatedAt"], "2026-04-12T08:00:06Z")
	}

	rawTurns, ok := snapshot["turns"].([]map[string]any)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns type = %T, want []map[string]any", snapshot["turns"])
	}
	if len(rawTurns) != 1 {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns len = %d, want 1", len(rawTurns))
	}
	if rawTurns[0]["turnId"] != "turn_002" {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].turnId = %v, want %q", rawTurns[0]["turnId"], "turn_002")
	}
	if rawTurns[0]["inputSummary"] != "implement feature x" {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].inputSummary = %v, want %q", rawTurns[0]["inputSummary"], "implement feature x")
	}
	if rawTurns[0]["runCount"] != 2 {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].runCount = %v, want %d", rawTurns[0]["runCount"], 2)
	}
	if rawTurns[0]["primaryRunId"] != "run_002" {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].primaryRunId = %v, want %q", rawTurns[0]["primaryRunId"], "run_002")
	}
	if rawTurns[0]["currentRunId"] != "run_003" {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].currentRunId = %v, want %q", rawTurns[0]["currentRunId"], "run_003")
	}
	if rawTurns[0]["latestRunId"] != "run_003" {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].latestRunId = %v, want %q", rawTurns[0]["latestRunId"], "run_003")
	}
	if rawTurns[0]["latestEventType"] != session.EventRunExternalMessageEnd {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].latestEventType = %v, want %q", rawTurns[0]["latestEventType"], session.EventRunExternalMessageEnd)
	}
	if rawTurns[0]["displayState"] != "external_running" {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].displayState = %v, want %q", rawTurns[0]["displayState"], "external_running")
	}
	activeRunIDs, ok := rawTurns[0]["activeRunIds"].([]string)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].activeRunIds type = %T, want []string", rawTurns[0]["activeRunIds"])
	}
	if len(activeRunIDs) != 2 || activeRunIDs[0] != "run_002" || activeRunIDs[1] != "run_003" {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].activeRunIds = %v, want [run_002 run_003]", activeRunIDs)
	}
	if rawTurns[0]["activeRunCount"] != 2 {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].activeRunCount = %v, want %d", rawTurns[0]["activeRunCount"], 2)
	}
	completedRunIDs, ok := rawTurns[0]["completedRunIds"].([]string)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].completedRunIds type = %T, want []string", rawTurns[0]["completedRunIds"])
	}
	if len(completedRunIDs) != 0 {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].completedRunIds len = %d, want 0", len(completedRunIDs))
	}
	if rawTurns[0]["completedRunCount"] != 0 {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].completedRunCount = %v, want %d", rawTurns[0]["completedRunCount"], 0)
	}
	failedRunIDs, ok := rawTurns[0]["failedRunIds"].([]string)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].failedRunIds type = %T, want []string", rawTurns[0]["failedRunIds"])
	}
	if len(failedRunIDs) != 0 {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].failedRunIds len = %d, want 0", len(failedRunIDs))
	}
	if rawTurns[0]["failedRunCount"] != 0 {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].failedRunCount = %v, want %d", rawTurns[0]["failedRunCount"], 0)
	}
	if rawTurns[0]["hasExternalWork"] != true {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].hasExternalWork = %v, want true", rawTurns[0]["hasExternalWork"])
	}
	if rawTurns[0]["latestMessagePreview"] != "hello world" {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].latestMessagePreview = %v, want %q", rawTurns[0]["latestMessagePreview"], "hello world")
	}

	rawRuns, ok := snapshot["runs"].([]map[string]any)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs type = %T, want []map[string]any", snapshot["runs"])
	}
	if len(rawRuns) != 2 {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs len = %d, want 2", len(rawRuns))
	}
	if rawRuns[0]["runId"] != "run_002" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].runId = %v, want %q", rawRuns[0]["runId"], "run_002")
	}
	if rawRuns[1]["runId"] != "run_003" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[1].runId = %v, want %q", rawRuns[1]["runId"], "run_003")
	}
	if rawRuns[1]["turnId"] != "turn_002" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[1].turnId = %v, want %q", rawRuns[1]["turnId"], "turn_002")
	}
	if rawRuns[1]["eventCount"] != 5 {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[1].eventCount = %v, want %d", rawRuns[1]["eventCount"], 5)
	}
	if rawRuns[1]["displayState"] != "external_running" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[1].displayState = %v, want %q", rawRuns[1]["displayState"], "external_running")
	}
	if rawRuns[1]["latestEventType"] != session.EventRunExternalMessageEnd {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[1].latestEventType = %v, want %q", rawRuns[1]["latestEventType"], session.EventRunExternalMessageEnd)
	}
	if rawRuns[1]["latestMessageId"] != "msg_001" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[1].latestMessageId = %v, want %q", rawRuns[1]["latestMessageId"], "msg_001")
	}
	if rawRuns[1]["latestMessageText"] != "hello world" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[1].latestMessageText = %v, want %q", rawRuns[1]["latestMessageText"], "hello world")
	}
	if rawRuns[1]["summary"] != "" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[1].summary = %v, want empty", rawRuns[1]["summary"])
	}
	externalTaskIDs, ok := rawRuns[1]["externalTaskIds"].([]string)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[1].externalTaskIds type = %T, want []string", rawRuns[1]["externalTaskIds"])
	}
	if len(externalTaskIDs) != 1 || externalTaskIDs[0] != "ext_001" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[1].externalTaskIds = %v, want [%q]", externalTaskIDs, "ext_001")
	}
	if rawRuns[1]["updatedAt"] != "2026-04-12T08:00:06Z" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[1].updatedAt = %v, want %q", rawRuns[1]["updatedAt"], "2026-04-12T08:00:06Z")
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
			"summary":        "done",
			"externalTaskId": "ext_001",
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

	activityContent, ok := activityEvent.Content.(map[string]any)
	if !ok {
		t.Fatalf("MapEvent().ActivitySnapshot.Content type = %T, want map[string]any", activityEvent.Content)
	}
	if activityContent["runId"] != "run_001" {
		t.Fatalf("MapEvent().ActivitySnapshot.Content.runId = %v, want %q", activityContent["runId"], "run_001")
	}
	if activityContent["externalTaskId"] != "ext_001" {
		t.Fatalf("MapEvent().ActivitySnapshot.Content.externalTaskId = %v, want %q", activityContent["externalTaskId"], "ext_001")
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
			"error":          "runner failed",
			"externalTaskId": "ext_001",
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
	if content["runId"] != "run_001" {
		t.Fatalf("MapEvent().Content.runId = %v, want %q", content["runId"], "run_001")
	}
	if content["externalTaskId"] != "ext_001" {
		t.Fatalf("MapEvent().Content.externalTaskId = %v, want %q", content["externalTaskId"], "ext_001")
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
			"externalTaskId": "ext_001",
			"kind":           "item_started",
			"itemId":         "item_001",
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
	if customEvent.Name != "suag.run.external.activity" {
		t.Fatalf("MapEvent()[0].Name = %q, want %q", customEvent.Name, "suag.run.external.activity")
	}
	value, ok := customEvent.Value.(map[string]any)
	if !ok {
		t.Fatalf("MapEvent()[0].Value type = %T, want map[string]any", customEvent.Value)
	}
	if value["externalTaskId"] != "ext_001" {
		t.Fatalf("MapEvent()[0].Value.externalTaskId = %v, want %q", value["externalTaskId"], "ext_001")
	}
	activityEvent, ok := got[1].(*ActivitySnapshotEvent)
	if !ok {
		t.Fatalf("MapEvent()[1] type = %T, want %T", got[1], &ActivitySnapshotEvent{})
	}
	if activityEvent.ActivityType != "external_activity" {
		t.Fatalf("MapEvent()[1].ActivityType = %q, want %q", activityEvent.ActivityType, "external_activity")
	}
	content, ok := activityEvent.Content.(map[string]any)
	if !ok {
		t.Fatalf("MapEvent()[1].Content type = %T, want map[string]any", activityEvent.Content)
	}
	if content["externalTaskId"] != "ext_001" {
		t.Fatalf("MapEvent()[1].Content.externalTaskId = %v, want %q", content["externalTaskId"], "ext_001")
	}
	if content["status"] != "external_running" {
		t.Fatalf("MapEvent()[1].Content.status = %v, want %q", content["status"], "external_running")
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

func TestMapEvent_RunStatusMapsToCustomAndSnapshot(t *testing.T) {
	t.Parallel()

	event := HostEvent{
		Type:      session.EventRunStatus,
		SessionID: "sess_001",
		TurnID:    "turn_001",
		RunID:     "run_001",
		AgentID:   "default",
		Sequence:  3,
		Payload: map[string]any{
			"state": string(session.RunInputRequired),
			"waitMetadata": map[string]any{
				"wait_kind": "input_required",
				"policy":    "manual",
			},
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
	if customEvent.Name != "suag.run.status" {
		t.Fatalf("MapEvent()[0].Name = %q, want %q", customEvent.Name, "suag.run.status")
	}

	activityEvent, ok := got[1].(*ActivitySnapshotEvent)
	if !ok {
		t.Fatalf("MapEvent()[1] type = %T, want %T", got[1], &ActivitySnapshotEvent{})
	}
	content, ok := activityEvent.Content.(map[string]any)
	if !ok {
		t.Fatalf("MapEvent()[1].Content type = %T, want map[string]any", activityEvent.Content)
	}
	if content["status"] != "input_required" {
		t.Fatalf("MapEvent()[1].Content.status = %v, want %q", content["status"], "input_required")
	}
	waitMetadata, ok := content["waitMetadata"].(map[string]any)
	if !ok {
		t.Fatalf("MapEvent()[1].Content.waitMetadata type = %T, want map[string]any", content["waitMetadata"])
	}
	if waitMetadata["policy"] != "manual" {
		t.Fatalf("MapEvent()[1].Content.waitMetadata[policy] = %v, want %q", waitMetadata["policy"], "manual")
	}
}

func TestMapEvent_RunStatusIncludesLoopPhaseMetadata(t *testing.T) {
	t.Parallel()

	event := HostEvent{
		Type:      session.EventRunStatus,
		SessionID: "sess_loop",
		TurnID:    "turn_loop",
		RunID:     "run_loop",
		AgentID:   "default",
		Sequence:  9,
		Payload: map[string]any{
			"state": string(session.RunInputRequired),
			"waitMetadata": map[string]any{
				"wait_kind": "input_required",
			},
		},
		RunMetadata: map[string]any{
			"loop_phase":    "resumed",
			"resume_source": "continue_turn",
			"continue_mode": "message",
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
	customValue, ok := customEvent.Value.(map[string]any)
	if !ok {
		t.Fatalf("MapEvent()[0].Value type = %T, want map[string]any", customEvent.Value)
	}
	customLoop, ok := customValue["loopMetadata"].(map[string]any)
	if !ok {
		t.Fatalf("MapEvent()[0].Value.loopMetadata type = %T, want map[string]any", customValue["loopMetadata"])
	}
	if customLoop["loop_phase"] != "resumed" {
		t.Fatalf("MapEvent()[0].Value.loopMetadata[loop_phase] = %v, want %q", customLoop["loop_phase"], "resumed")
	}
	if customLoop["resume_source"] != "continue_turn" {
		t.Fatalf("MapEvent()[0].Value.loopMetadata[resume_source] = %v, want %q", customLoop["resume_source"], "continue_turn")
	}

	activityEvent, ok := got[1].(*ActivitySnapshotEvent)
	if !ok {
		t.Fatalf("MapEvent()[1] type = %T, want %T", got[1], &ActivitySnapshotEvent{})
	}
	content, ok := activityEvent.Content.(map[string]any)
	if !ok {
		t.Fatalf("MapEvent()[1].Content type = %T, want map[string]any", activityEvent.Content)
	}
	loopMetadata, ok := content["loopMetadata"].(map[string]any)
	if !ok {
		t.Fatalf("MapEvent()[1].Content.loopMetadata type = %T, want map[string]any", content["loopMetadata"])
	}
	if loopMetadata["continue_mode"] != "message" {
		t.Fatalf("MapEvent()[1].Content.loopMetadata[continue_mode] = %v, want %q", loopMetadata["continue_mode"], "message")
	}
}

func TestMapEvent_RunFinishedIncludesPostTurnHints(t *testing.T) {
	t.Parallel()

	event := HostEvent{
		Type:      session.EventRunFinished,
		SessionID: "sess_post",
		TurnID:    "turn_post",
		RunID:     "run_post",
		AgentID:   "default",
		Payload: map[string]any{
			"summary": "final answer",
		},
		RunMetadata: map[string]any{
			"turn_outcome": "completed",
			"planner_hint": map[string]any{
				"stage":   "review",
				"summary": "review the turn",
			},
			"next_action_hint": map[string]any{
				"kind":    "review",
				"title":   "Review Needed",
				"message": "Inspect the completed run.",
			},
		},
	}

	got := MapEvent(event)
	if len(got) != 5 {
		t.Fatalf("MapEvent() len = %d, want 5", len(got))
	}

	activityEvent, ok := got[1].(*ActivitySnapshotEvent)
	if !ok {
		t.Fatalf("MapEvent()[1] type = %T, want %T", got[1], &ActivitySnapshotEvent{})
	}
	content, ok := activityEvent.Content.(map[string]any)
	if !ok {
		t.Fatalf("MapEvent()[1].Content type = %T, want map[string]any", activityEvent.Content)
	}
	if content["turnOutcome"] != "completed" {
		t.Fatalf("MapEvent()[1].Content.turnOutcome = %v, want %q", content["turnOutcome"], "completed")
	}
	plannerHint, ok := content["plannerHint"].(map[string]any)
	if !ok {
		t.Fatalf("MapEvent()[1].Content.plannerHint type = %T, want map[string]any", content["plannerHint"])
	}
	if plannerHint["stage"] != "review" {
		t.Fatalf("MapEvent()[1].Content.plannerHint[stage] = %v, want %q", plannerHint["stage"], "review")
	}
	nextActionHint, ok := content["nextActionHint"].(map[string]any)
	if !ok {
		t.Fatalf("MapEvent()[1].Content.nextActionHint type = %T, want map[string]any", content["nextActionHint"])
	}
	if nextActionHint["message"] != "Inspect the completed run." {
		t.Fatalf("MapEvent()[1].Content.nextActionHint[message] = %v, want message", nextActionHint["message"])
	}
}

func TestBuildStateSnapshotUsesRunStatusDisplayState(t *testing.T) {
	t.Parallel()

	sessionModel := session.Session{
		SessionID:  "sess_status",
		Title:      "status session",
		Status:     session.SessionWaiting,
		LastTurnID: "turn_status",
	}
	events := []HostEvent{
		{
			Type:      session.EventRunQueued,
			SessionID: "sess_status",
			TurnID:    "turn_status",
			RunID:     "run_status",
			AgentID:   "default",
			Sequence:  1,
			CreatedAt: time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC),
		},
		{
			Type:      session.EventRunStarted,
			SessionID: "sess_status",
			TurnID:    "turn_status",
			RunID:     "run_status",
			AgentID:   "default",
			Sequence:  2,
			CreatedAt: time.Date(2026, 4, 15, 10, 0, 1, 0, time.UTC),
		},
		{
			Type:      session.EventRunStatus,
			SessionID: "sess_status",
			TurnID:    "turn_status",
			RunID:     "run_status",
			AgentID:   "default",
			Sequence:  3,
			CreatedAt: time.Date(2026, 4, 15, 10, 0, 2, 0, time.UTC),
			Payload: map[string]any{
				"state": string(session.RunAuthRequired),
				"waitMetadata": map[string]any{
					"wait_kind": "auth_required",
					"policy":    "manual_review",
				},
			},
		},
	}

	got := BuildStateSnapshot(sessionModel, events, nil)
	snapshot, ok := got.Snapshot.(map[string]any)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot type = %T, want map[string]any", got.Snapshot)
	}

	rawTurns, ok := snapshot["turns"].([]map[string]any)
	if !ok || len(rawTurns) != 1 {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns = %T/%d, want []map[string]any len 1", snapshot["turns"], len(rawTurns))
	}
	if rawTurns[0]["displayState"] != "auth_required" {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].displayState = %v, want %q", rawTurns[0]["displayState"], "auth_required")
	}

	rawRuns, ok := snapshot["runs"].([]map[string]any)
	if !ok || len(rawRuns) != 1 {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs = %T/%d, want []map[string]any len 1", snapshot["runs"], len(rawRuns))
	}
	if rawRuns[0]["displayState"] != "auth_required" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].displayState = %v, want %q", rawRuns[0]["displayState"], "auth_required")
	}
	waitMetadata, ok := rawRuns[0]["waitMetadata"].(map[string]any)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].waitMetadata type = %T, want map[string]any", rawRuns[0]["waitMetadata"])
	}
	if waitMetadata["policy"] != "manual_review" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].waitMetadata[policy] = %v, want %q", waitMetadata["policy"], "manual_review")
	}
}

func TestBuildStateSnapshotIncludesLoopPhaseMetadataForWaitingRun(t *testing.T) {
	t.Parallel()

	sessionModel := session.Session{
		SessionID:  "sess_wait_loop",
		Title:      "wait loop session",
		Status:     session.SessionWaiting,
		LastTurnID: "turn_wait_loop",
	}
	events := []HostEvent{
		{
			Type:      session.EventRunQueued,
			SessionID: "sess_wait_loop",
			TurnID:    "turn_wait_loop",
			RunID:     "run_wait_loop",
			AgentID:   "default",
			Sequence:  1,
			CreatedAt: time.Date(2026, 4, 20, 13, 0, 0, 0, time.UTC),
			RunMetadata: map[string]any{
				"loop_phase":    "resumed",
				"resume_source": "continue_turn",
				"continue_mode": "message",
			},
		},
		{
			Type:      session.EventRunStatus,
			SessionID: "sess_wait_loop",
			TurnID:    "turn_wait_loop",
			RunID:     "run_wait_loop",
			AgentID:   "default",
			Sequence:  2,
			CreatedAt: time.Date(2026, 4, 20, 13, 0, 1, 0, time.UTC),
			Payload: map[string]any{
				"state": string(session.RunInputRequired),
				"waitMetadata": map[string]any{
					"wait_kind": "input_required",
				},
			},
			RunMetadata: map[string]any{
				"loop_phase":    "resumed",
				"resume_source": "continue_turn",
				"continue_mode": "message",
			},
		},
	}

	got := BuildStateSnapshot(sessionModel, events, nil)
	snapshot, ok := got.Snapshot.(map[string]any)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot type = %T, want map[string]any", got.Snapshot)
	}

	rawRuns, ok := snapshot["runs"].([]map[string]any)
	if !ok || len(rawRuns) != 1 {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs = %T/%d, want []map[string]any len 1", snapshot["runs"], len(rawRuns))
	}
	loopMetadata, ok := rawRuns[0]["loopMetadata"].(map[string]any)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].loopMetadata type = %T, want map[string]any", rawRuns[0]["loopMetadata"])
	}
	if loopMetadata["loop_phase"] != "resumed" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].loopMetadata[loop_phase] = %v, want %q", loopMetadata["loop_phase"], "resumed")
	}
	if loopMetadata["resume_source"] != "continue_turn" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].loopMetadata[resume_source] = %v, want %q", loopMetadata["resume_source"], "continue_turn")
	}
	waitMetadata, ok := rawRuns[0]["waitMetadata"].(map[string]any)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].waitMetadata type = %T, want map[string]any", rawRuns[0]["waitMetadata"])
	}
	if waitMetadata["wait_kind"] != "input_required" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].waitMetadata[wait_kind] = %v, want %q", waitMetadata["wait_kind"], "input_required")
	}
}

func TestBuildStateSnapshotIncludesPostTurnHintsForTerminalRun(t *testing.T) {
	t.Parallel()

	sessionModel := session.Session{
		SessionID:  "sess_terminal_post",
		Title:      "terminal post session",
		Status:     session.SessionOpen,
		LastTurnID: "turn_terminal_post",
	}
	events := []HostEvent{
		{
			Type:      session.EventRunStarted,
			SessionID: "sess_terminal_post",
			TurnID:    "turn_terminal_post",
			RunID:     "run_terminal_post",
			AgentID:   "default",
			Sequence:  1,
			CreatedAt: time.Date(2026, 4, 20, 13, 5, 0, 0, time.UTC),
			RunMetadata: map[string]any{
				"loop_phase":    "initial",
				"continue_mode": "start",
			},
		},
		{
			Type:      session.EventRunFinished,
			SessionID: "sess_terminal_post",
			TurnID:    "turn_terminal_post",
			RunID:     "run_terminal_post",
			AgentID:   "default",
			Sequence:  2,
			CreatedAt: time.Date(2026, 4, 20, 13, 5, 1, 0, time.UTC),
			Payload: map[string]any{
				"summary": "terminal summary",
			},
			RunMetadata: map[string]any{
				"loop_phase":   "initial",
				"turn_outcome": "completed",
				"planner_hint": map[string]any{
					"stage": "review",
				},
				"next_action_hint": map[string]any{
					"kind":    "review",
					"message": "Inspect the completed run.",
				},
			},
		},
	}

	got := BuildStateSnapshot(sessionModel, events, nil)
	snapshot, ok := got.Snapshot.(map[string]any)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot type = %T, want map[string]any", got.Snapshot)
	}

	rawRuns, ok := snapshot["runs"].([]map[string]any)
	if !ok || len(rawRuns) != 1 {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs = %T/%d, want []map[string]any len 1", snapshot["runs"], len(rawRuns))
	}
	if rawRuns[0]["turnOutcome"] != "completed" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].turnOutcome = %v, want %q", rawRuns[0]["turnOutcome"], "completed")
	}
	nextActionHint, ok := rawRuns[0]["nextActionHint"].(map[string]any)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].nextActionHint type = %T, want map[string]any", rawRuns[0]["nextActionHint"])
	}
	if nextActionHint["kind"] != "review" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].nextActionHint[kind] = %v, want %q", nextActionHint["kind"], "review")
	}
	plannerHint, ok := rawRuns[0]["plannerHint"].(map[string]any)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].plannerHint type = %T, want map[string]any", rawRuns[0]["plannerHint"])
	}
	if plannerHint["stage"] != "review" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].plannerHint[stage] = %v, want %q", plannerHint["stage"], "review")
	}
}

func TestBuildStateSnapshotIncludesMemoryRecallMetadata(t *testing.T) {
	t.Parallel()

	sessionModel := session.Session{
		SessionID:  "sess_memory_recall",
		Title:      "memory recall session",
		Status:     session.SessionOpen,
		LastTurnID: "turn_memory_recall",
	}
	events := []HostEvent{
		{
			Type:      session.EventRunFinished,
			SessionID: "sess_memory_recall",
			TurnID:    "turn_memory_recall",
			RunID:     "run_memory_recall",
			AgentID:   "default",
			Sequence:  1,
			CreatedAt: time.Date(2026, 4, 20, 15, 0, 0, 0, time.UTC),
			Payload: map[string]any{
				"summary": "done",
			},
			RunMetadata: map[string]any{
				"memory_recall": map[string]any{
					"enabled":     true,
					"entry_count": 3,
					"sources": []map[string]any{
						{
							"name":        "MEMORY",
							"entry_count": 2,
							"preview":     []string{"- [turn_learning] prefer concise notes"},
						},
						{
							"name":        "USER",
							"entry_count": 1,
							"preview":     []string{"- [user_preference] 优先中文回复"},
						},
					},
				},
			},
		},
	}

	got := BuildStateSnapshot(sessionModel, events, nil)
	snapshot, ok := got.Snapshot.(map[string]any)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot type = %T, want map[string]any", got.Snapshot)
	}

	rawRuns, ok := snapshot["runs"].([]map[string]any)
	if !ok || len(rawRuns) != 1 {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs = %T/%d, want []map[string]any len 1", snapshot["runs"], len(rawRuns))
	}
	recall, ok := rawRuns[0]["memoryRecall"].(map[string]any)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].memoryRecall type = %T, want map[string]any", rawRuns[0]["memoryRecall"])
	}
	if recall["entry_count"] != 3 {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].memoryRecall[entry_count] = %v, want %d", recall["entry_count"], 3)
	}
	sources, ok := recall["sources"].([]map[string]any)
	if !ok || len(sources) != 2 {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].memoryRecall[sources] = %T/%d, want []map[string]any len 2", recall["sources"], len(sources))
	}
	if sources[0]["name"] != "MEMORY" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].memoryRecall[sources][0][name] = %v, want %q", sources[0]["name"], "MEMORY")
	}
}

func TestBuildStateSnapshotIncludesFailedRunErrorMessage(t *testing.T) {
	t.Parallel()

	sessionModel := session.Session{
		SessionID:  "sess_failed",
		Title:      "failed session",
		Status:     session.SessionWaiting,
		LastTurnID: "turn_failed",
	}
	events := []HostEvent{
		{
			Type:      session.EventRunStarted,
			SessionID: "sess_failed",
			TurnID:    "turn_failed",
			RunID:     "run_failed",
			AgentID:   "default",
			Sequence:  1,
			CreatedAt: time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC),
		},
		{
			Type:      session.EventRunFailed,
			SessionID: "sess_failed",
			TurnID:    "turn_failed",
			RunID:     "run_failed",
			AgentID:   "default",
			Sequence:  2,
			CreatedAt: time.Date(2026, 4, 18, 10, 0, 1, 0, time.UTC),
			Payload: map[string]any{
				"error": "tool call \"file_read\" arguments must be a JSON object",
			},
		},
	}

	got := BuildStateSnapshot(sessionModel, events, nil)
	snapshot, ok := got.Snapshot.(map[string]any)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot type = %T, want map[string]any", got.Snapshot)
	}

	rawTurns, ok := snapshot["turns"].([]map[string]any)
	if !ok || len(rawTurns) != 1 {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns = %T/%d, want []map[string]any len 1", snapshot["turns"], len(rawTurns))
	}
	if rawTurns[0]["displayState"] != "failed" {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].displayState = %v, want %q", rawTurns[0]["displayState"], "failed")
	}
	if rawTurns[0]["latestMessagePreview"] != "tool call \"file_read\" arguments must be a JSON object" {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].latestMessagePreview = %v, want failure message", rawTurns[0]["latestMessagePreview"])
	}

	rawRuns, ok := snapshot["runs"].([]map[string]any)
	if !ok || len(rawRuns) != 1 {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs = %T/%d, want []map[string]any len 1", snapshot["runs"], len(rawRuns))
	}
	if rawRuns[0]["displayState"] != "failed" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].displayState = %v, want %q", rawRuns[0]["displayState"], "failed")
	}
	if rawRuns[0]["summary"] != "" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].summary = %v, want empty", rawRuns[0]["summary"])
	}
	if rawRuns[0]["latestMessageText"] != "tool call \"file_read\" arguments must be a JSON object" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].latestMessageText = %v, want failure message", rawRuns[0]["latestMessageText"])
	}
}

func TestBuildStateSnapshotIncludesCanceledRunReason(t *testing.T) {
	t.Parallel()

	sessionModel := session.Session{
		SessionID:  "sess_canceled",
		Title:      "canceled session",
		Status:     session.SessionOpen,
		LastTurnID: "turn_canceled",
	}
	events := []HostEvent{
		{
			Type:      session.EventRunStarted,
			SessionID: "sess_canceled",
			TurnID:    "turn_canceled",
			RunID:     "run_canceled",
			AgentID:   "default",
			Sequence:  1,
			CreatedAt: time.Date(2026, 4, 19, 14, 0, 0, 0, time.UTC),
		},
		{
			Type:      session.EventRunCanceled,
			SessionID: "sess_canceled",
			TurnID:    "turn_canceled",
			RunID:     "run_canceled",
			AgentID:   "default",
			Sequence:  2,
			CreatedAt: time.Date(2026, 4, 19, 14, 0, 1, 0, time.UTC),
			Payload: map[string]any{
				"reason": "stopped by operator",
			},
		},
	}

	got := BuildStateSnapshot(sessionModel, events, nil)
	snapshot, ok := got.Snapshot.(map[string]any)
	if !ok {
		t.Fatalf("BuildStateSnapshot().Snapshot type = %T, want map[string]any", got.Snapshot)
	}

	rawTurns, ok := snapshot["turns"].([]map[string]any)
	if !ok || len(rawTurns) != 1 {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns = %T/%d, want []map[string]any len 1", snapshot["turns"], len(rawTurns))
	}
	if rawTurns[0]["displayState"] != "canceled" {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].displayState = %v, want %q", rawTurns[0]["displayState"], "canceled")
	}
	if rawTurns[0]["latestMessagePreview"] != "stopped by operator" {
		t.Fatalf("BuildStateSnapshot().Snapshot.turns[0].latestMessagePreview = %v, want cancel reason", rawTurns[0]["latestMessagePreview"])
	}

	rawRuns, ok := snapshot["runs"].([]map[string]any)
	if !ok || len(rawRuns) != 1 {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs = %T/%d, want []map[string]any len 1", snapshot["runs"], len(rawRuns))
	}
	if rawRuns[0]["displayState"] != "canceled" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].displayState = %v, want %q", rawRuns[0]["displayState"], "canceled")
	}
	if rawRuns[0]["latestMessageText"] != "stopped by operator" {
		t.Fatalf("BuildStateSnapshot().Snapshot.runs[0].latestMessageText = %v, want cancel reason", rawRuns[0]["latestMessageText"])
	}
}
