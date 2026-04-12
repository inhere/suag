package agui

import (
	"time"

	"github.com/inhere/axon/internal/host/session"
)

func MapEvent(event HostEvent) []Event {
	now := time.Now().UnixMilli()

	switch event.Type {
	case session.EventRunStarted:
		return []Event{
			withTimestamp(NewRunStartedEvent(event.SessionID, event.RunID), now),
			buildRunActivitySnapshot(now, event, "running"),
		}
	case session.EventRunFinished:
		textEvents := buildFinishedTextEvents(now, event)
		out := []Event{
			withTimestamp(NewRunFinishedEvent(event.SessionID, event.RunID, event.Payload), now),
			buildRunActivitySnapshot(now, event, "completed"),
		}
		out = append(out, textEvents...)
		return out
	case session.EventRunFailed:
		message := "run failed"
		if payload, ok := event.Payload.(map[string]any); ok {
			if raw, ok := payload["error"].(string); ok && raw != "" {
				message = raw
			}
		}
		return []Event{
			withTimestamp(NewRunErrorEvent(event.RunID, message), now),
			buildRunActivitySnapshot(now, event, "failed"),
		}
	case session.EventRunExternalActivity:
		return []Event{
			withTimestamp(NewCustomEvent("axon.run.external.activity", map[string]any{
				"threadId":       event.SessionID,
				"sessionId":      event.SessionID,
				"turnId":         event.TurnID,
				"runId":          event.RunID,
				"agentId":        event.AgentID,
				"sequence":       event.Sequence,
				"type":           event.Type,
				"payload":        event.Payload,
				"externalTaskId": externalTaskIDFromPayload(event.Payload),
			}), now),
			withTimestamp(NewActivitySnapshotEvent(event.RunID+":external", "external_activity", map[string]any{
				"threadId":       event.SessionID,
				"turnId":         event.TurnID,
				"runId":          event.RunID,
				"agentId":        event.AgentID,
				"status":         "running",
				"payload":        event.Payload,
				"externalTaskId": externalTaskIDFromPayload(event.Payload),
			}), now),
		}
	case session.EventRunExternalMessageDelta:
		return buildExternalMessageDeltaEvents(now, event)
	case session.EventRunExternalMessageEnd:
		return buildExternalMessageEndEvents(now, event)
	}

	name := "axon.event"
	if event.Type == session.EventRunQueued {
		name = "axon.run.queued"
	}

	return []Event{
		withTimestamp(NewCustomEvent(name, map[string]any{
			"threadId":       event.SessionID,
			"sessionId":      event.SessionID,
			"turnId":         event.TurnID,
			"runId":          event.RunID,
			"agentId":        event.AgentID,
			"sequence":       event.Sequence,
			"type":           event.Type,
			"payload":        event.Payload,
			"externalTaskId": externalTaskIDFromPayload(event.Payload),
		}), now),
		buildRunActivitySnapshot(now, event, "queued"),
	}
}

func buildExternalMessageDeltaEvents(now int64, event HostEvent) []Event {
	payload, ok := event.Payload.(map[string]any)
	if !ok {
		return nil
	}

	messageID, _ := payload["messageId"].(string)
	if messageID == "" {
		return nil
	}
	delta, _ := payload["delta"].(string)
	isFirstChunk, _ := payload["isFirstChunk"].(bool)

	out := make([]Event, 0, 2)
	if isFirstChunk {
		out = append(out, withTimestamp(NewTextMessageStartEvent(messageID, "assistant"), now))
	}
	if delta != "" {
		out = append(out, withTimestamp(NewTextMessageContentEvent(messageID, delta), now))
	}
	return out
}

func buildExternalMessageEndEvents(now int64, event HostEvent) []Event {
	payload, ok := event.Payload.(map[string]any)
	if !ok {
		return nil
	}

	messageID, _ := payload["messageId"].(string)
	if messageID == "" {
		return nil
	}

	return []Event{
		withTimestamp(NewTextMessageEndEvent(messageID), now),
	}
}

func buildFinishedTextEvents(now int64, event HostEvent) []Event {
	payload, ok := event.Payload.(map[string]any)
	if !ok {
		return nil
	}

	summary, ok := payload["summary"].(string)
	if !ok || summary == "" {
		return nil
	}

	messageID := event.RunID + ":assistant"
	return []Event{
		withTimestamp(NewTextMessageStartEvent(messageID, "assistant"), now),
		withTimestamp(NewTextMessageContentEvent(messageID, summary), now),
		withTimestamp(NewTextMessageEndEvent(messageID), now),
	}
}

func BuildStateSnapshot(sessionModel session.Session, events []HostEvent, externalTasks []session.ExternalTask) StateSnapshotEvent {
	now := time.Now().UnixMilli()

	mappedEvents := make([]map[string]any, 0, len(events))
	mappedExternalTasks := make([]map[string]any, 0, len(externalTasks))
	mappedExternalActivities := make([]map[string]any, 0)
	mappedExternalMessages := make([]map[string]any, 0)
	mappedTurns := make([]map[string]any, 0)
	mappedRuns := make([]map[string]any, 0)
	externalActivityIndex := make(map[string]int)
	externalMessageIndex := make(map[string]int)
	turnIndex := make(map[string]int)
	runIndex := make(map[string]int)
	currentRunID := ""
	currentAgentID := ""
	latestEventType := ""
	for _, event := range events {
		currentRunID = event.RunID
		currentAgentID = event.AgentID
		latestEventType = event.Type

		mappedEvents = append(mappedEvents, map[string]any{
			"type":      event.Type,
			"sessionId": event.SessionID,
			"turnId":    event.TurnID,
			"runId":     event.RunID,
			"agentId":   event.AgentID,
			"sequence":  event.Sequence,
			"createdAt": formatEventTime(event.CreatedAt),
			"payload":   event.Payload,
		})

		turnKey := event.TurnID
		if turnKey != "" {
			idx, ok := turnIndex[turnKey]
			if !ok {
				mappedTurns = append(mappedTurns, map[string]any{
					"turnId":               event.TurnID,
					"sessionId":            event.SessionID,
					"primaryRunId":         "",
					"currentRunId":         "",
					"latestRunId":          "",
					"latestAgentId":        "",
					"displayState":         "",
					"latestEventType":      "",
					"latestMessagePreview": "",
					"eventCount":           0,
					"runCount":             0,
					"activeRunCount":       0,
					"completedRunCount":    0,
					"failedRunCount":       0,
					"activeRunIds":         []string{},
					"completedRunIds":      []string{},
					"failedRunIds":         []string{},
					"hasExternalWork":      false,
					"updatedAt":            "",
				})
				idx = len(mappedTurns) - 1
				turnIndex[turnKey] = idx
			}
			mergeTurnSnapshot(mappedTurns[idx], event)
		}

		runKey := event.RunID
		if runKey != "" {
			idx, ok := runIndex[runKey]
			if !ok {
				mappedRuns = append(mappedRuns, map[string]any{
					"runId":             event.RunID,
					"turnId":            event.TurnID,
					"sessionId":         event.SessionID,
					"agentId":           event.AgentID,
					"displayState":      "",
					"latestEventType":   "",
					"latestMessageId":   "",
					"latestMessageText": "",
					"summary":           "",
					"externalTaskIds":   []string{},
					"eventCount":        0,
					"updatedAt":         "",
				})
				idx = len(mappedRuns) - 1
				runIndex[runKey] = idx
				if turnKey != "" {
					turnSnapshot := mappedTurns[turnIndex[turnKey]]
					turnSnapshot["runCount"] = turnSnapshot["runCount"].(int) + 1
				}
			}
			mergeRunSnapshot(mappedRuns[idx], event)
		}

		switch event.Type {
		case session.EventRunExternalActivity:
			key := externalActivityKey(event)
			idx, ok := externalActivityIndex[key]
			if !ok {
				mappedExternalActivities = append(mappedExternalActivities, map[string]any{
					"externalTaskId": externalTaskIDFromPayload(event.Payload),
					"sessionId":      event.SessionID,
					"turnId":         event.TurnID,
					"runId":          event.RunID,
					"agentId":        event.AgentID,
				})
				idx = len(mappedExternalActivities) - 1
				externalActivityIndex[key] = idx
			}
			mergeExternalActivity(mappedExternalActivities[idx], event)
		case session.EventRunExternalMessageDelta, session.EventRunExternalMessageEnd:
			key := externalMessageKey(event)
			if key == "" {
				continue
			}
			idx, ok := externalMessageIndex[key]
			if !ok {
				mappedExternalMessages = append(mappedExternalMessages, map[string]any{
					"externalTaskId": externalTaskIDFromPayload(event.Payload),
					"messageId":      payloadString(event.Payload, "messageId"),
					"sessionId":      event.SessionID,
					"turnId":         event.TurnID,
					"runId":          event.RunID,
					"agentId":        event.AgentID,
					"content":        "",
					"started":        false,
					"completed":      false,
				})
				idx = len(mappedExternalMessages) - 1
				externalMessageIndex[key] = idx
			}
			mergeExternalMessage(mappedExternalMessages[idx], event)
		}
	}
	for _, task := range externalTasks {
		mappedExternalTasks = append(mappedExternalTasks, map[string]any{
			"externalTaskId":  task.ExternalTaskID,
			"sessionId":       task.SessionID,
			"turnId":          task.TurnID,
			"runId":           task.RunID,
			"adapter":         task.Adapter,
			"remoteAgentName": task.RemoteAgentName,
			"remoteSessionId": task.RemoteSessionID,
			"remoteTaskId":    task.RemoteTaskID,
			"state":           task.State,
			"lastHeartbeatAt": task.LastHeartbeatAt.UTC().Format(time.RFC3339Nano),
			"resultSummary":   task.ResultSummary,
			"metadata":        task.Metadata,
		})

		if idx, ok := runIndex[task.RunID]; ok {
			mergeRunExternalTask(mappedRuns[idx], task)
		}
	}

	for _, message := range mappedExternalMessages {
		mergeRunExternalMessage(mappedRuns, runIndex, message)
		mergeTurnMessagePreview(mappedTurns, turnIndex, message)
	}
	for _, run := range mappedRuns {
		mergeTurnSummaryPreview(mappedTurns, turnIndex, run)
	}

	return *withTimestamp(NewStateSnapshotEvent(map[string]any{
		"threadId":           sessionModel.SessionID,
		"sessionId":          sessionModel.SessionID,
		"title":              sessionModel.Title,
		"status":             string(sessionModel.Status),
		"turnId":             sessionModel.LastTurnID,
		"lastTurnId":         sessionModel.LastTurnID,
		"currentRunId":       currentRunID,
		"currentAgentId":     currentAgentID,
		"latestEventType":    latestEventType,
		"eventCount":         len(mappedEvents),
		"externalTasks":      mappedExternalTasks,
		"externalActivities": mappedExternalActivities,
		"externalMessages":   mappedExternalMessages,
		"turns":              mappedTurns,
		"runs":               mappedRuns,
		"events":             mappedEvents,
	}), now)
}

func buildRunActivitySnapshot(now int64, event HostEvent, status string) *ActivitySnapshotEvent {
	return withTimestamp(NewActivitySnapshotEvent(event.RunID, "run_status", map[string]any{
		"threadId": event.SessionID,
		"turnId":   event.TurnID,
		"runId":    event.RunID,
		"agentId":  event.AgentID,
		"status":   status,
	}), now)
}

func withTimestamp[T interface {
	SetTimestamp(timestamp int64)
}](event T, timestamp int64) T {
	event.SetTimestamp(timestamp)
	return event
}

func externalTaskIDFromPayload(payload any) string {
	values, ok := payload.(map[string]any)
	if !ok || len(values) == 0 {
		return ""
	}
	raw, ok := values["externalTaskId"]
	if !ok {
		return ""
	}
	text, _ := raw.(string)
	return text
}

func externalActivityKey(event HostEvent) string {
	externalTaskID := externalTaskIDFromPayload(event.Payload)
	if externalTaskID != "" {
		return externalTaskID
	}
	if event.RunID != "" {
		return event.RunID
	}
	return event.SessionID + ":" + event.Type
}

func externalMessageKey(event HostEvent) string {
	messageID := payloadString(event.Payload, "messageId")
	if messageID == "" {
		return ""
	}
	externalTaskID := externalTaskIDFromPayload(event.Payload)
	if externalTaskID == "" {
		return messageID
	}
	return externalTaskID + ":" + messageID
}

func mergeExternalActivity(target map[string]any, event HostEvent) {
	target["lastSequence"] = event.Sequence
	target["updatedAt"] = formatEventTime(event.CreatedAt)
	target["lastEventType"] = event.Type
	target["payload"] = event.Payload

	values, ok := event.Payload.(map[string]any)
	if !ok {
		return
	}
	if kind, _ := values["kind"].(string); kind != "" {
		target["lastKind"] = kind
	}
	target["lastPayload"] = values
}

func mergeExternalMessage(target map[string]any, event HostEvent) {
	target["lastSequence"] = event.Sequence
	target["updatedAt"] = formatEventTime(event.CreatedAt)
	target["lastEventType"] = event.Type

	values, ok := event.Payload.(map[string]any)
	if !ok {
		return
	}

	if isFirstChunk, _ := values["isFirstChunk"].(bool); isFirstChunk {
		target["started"] = true
	}
	if event.Type == session.EventRunExternalMessageEnd {
		target["completed"] = true
	}
	if delta, _ := values["delta"].(string); delta != "" {
		content, _ := target["content"].(string)
		target["content"] = content + delta
	}
}

func payloadString(payload any, key string) string {
	values, ok := payload.(map[string]any)
	if !ok || len(values) == 0 {
		return ""
	}
	text, _ := values[key].(string)
	return text
}

func mergeTurnSnapshot(target map[string]any, event HostEvent) {
	if target["primaryRunId"].(string) == "" {
		target["primaryRunId"] = event.RunID
	}
	target["currentRunId"] = event.RunID
	target["latestRunId"] = event.RunID
	target["latestAgentId"] = event.AgentID
	target["displayState"] = displayStateForEvent(event)
	target["latestEventType"] = event.Type
	target["eventCount"] = target["eventCount"].(int) + 1
	target["updatedAt"] = formatEventTime(event.CreatedAt)

	switch displayStateForEvent(event) {
	case "completed":
		removeStringFieldValue(target, "activeRunIds", event.RunID)
		appendUniqueStringField(target, "completedRunIds", event.RunID)
	case "failed":
		removeStringFieldValue(target, "activeRunIds", event.RunID)
		appendUniqueStringField(target, "failedRunIds", event.RunID)
	case "queued", "running", "external_running":
		appendUniqueStringField(target, "activeRunIds", event.RunID)
	}

	if event.Type == session.EventRunExternalActivity || event.Type == session.EventRunExternalMessageDelta || event.Type == session.EventRunExternalMessageEnd {
		target["hasExternalWork"] = true
	}
	target["activeRunCount"] = len(target["activeRunIds"].([]string))
	target["completedRunCount"] = len(target["completedRunIds"].([]string))
	target["failedRunCount"] = len(target["failedRunIds"].([]string))
}

func mergeRunSnapshot(target map[string]any, event HostEvent) {
	target["agentId"] = event.AgentID
	target["displayState"] = displayStateForEvent(event)
	target["latestEventType"] = event.Type
	target["eventCount"] = target["eventCount"].(int) + 1
	target["updatedAt"] = formatEventTime(event.CreatedAt)

	if event.Type != session.EventRunFinished {
		return
	}

	values, ok := event.Payload.(map[string]any)
	if !ok {
		return
	}
	summary, _ := values["summary"].(string)
	if summary == "" {
		return
	}

	target["summary"] = summary
	target["latestMessageId"] = event.RunID + ":assistant"
	target["latestMessageText"] = summary
}

func formatEventTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func mergeRunExternalTask(target map[string]any, task session.ExternalTask) {
	ids, _ := target["externalTaskIds"].([]string)
	for _, id := range ids {
		if id == task.ExternalTaskID {
			return
		}
	}
	target["externalTaskIds"] = append(ids, task.ExternalTaskID)
}

func appendUniqueStringField(target map[string]any, key, value string) {
	if value == "" {
		return
	}
	values, _ := target[key].([]string)
	for _, existing := range values {
		if existing == value {
			return
		}
	}
	target[key] = append(values, value)
}

func removeStringFieldValue(target map[string]any, key, value string) {
	values, _ := target[key].([]string)
	if len(values) == 0 || value == "" {
		return
	}

	filtered := values[:0]
	for _, existing := range values {
		if existing != value {
			filtered = append(filtered, existing)
		}
	}
	target[key] = append([]string{}, filtered...)
}

func mergeRunExternalMessage(runs []map[string]any, runIndex map[string]int, message map[string]any) {
	runID, _ := message["runId"].(string)
	if runID == "" {
		return
	}
	idx, ok := runIndex[runID]
	if !ok {
		return
	}

	if summary, _ := runs[idx]["summary"].(string); summary != "" {
		return
	}

	runs[idx]["latestMessageId"] = message["messageId"]
	runs[idx]["latestMessageText"] = message["content"]
	if runs[idx]["displayState"] == "" {
		runs[idx]["displayState"] = "running"
	}
}

func mergeTurnMessagePreview(turns []map[string]any, turnIndex map[string]int, message map[string]any) {
	turnID, _ := message["turnId"].(string)
	if turnID == "" {
		return
	}
	idx, ok := turnIndex[turnID]
	if !ok {
		return
	}

	turns[idx]["latestMessagePreview"] = message["content"]
}

func mergeTurnSummaryPreview(turns []map[string]any, turnIndex map[string]int, run map[string]any) {
	turnID, _ := run["turnId"].(string)
	summary, _ := run["summary"].(string)
	if turnID == "" || summary == "" {
		return
	}
	idx, ok := turnIndex[turnID]
	if !ok {
		return
	}

	turns[idx]["latestMessagePreview"] = summary
}

func displayStateForEvent(event HostEvent) string {
	switch event.Type {
	case session.EventRunQueued:
		return "queued"
	case session.EventRunStarted:
		return "running"
	case session.EventRunFinished:
		return "completed"
	case session.EventRunFailed:
		return "failed"
	case session.EventRunExternalActivity, session.EventRunExternalMessageDelta, session.EventRunExternalMessageEnd:
		return "external_running"
	default:
		return ""
	}
}
