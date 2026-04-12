package agui

import (
	"testing"
	"time"

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
			CreatedAt: time.Date(2026, 4, 12, 8, 0, 0, 0, time.UTC),
		},
		{
			Type:      "run.started",
			SessionID: "sess_001",
			TurnID:    "turn_002",
			RunID:     "run_002",
			AgentID:   "default",
			Sequence:  3,
			CreatedAt: time.Date(2026, 4, 12, 8, 0, 1, 0, time.UTC),
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
