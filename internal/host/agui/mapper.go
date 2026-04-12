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
			"payload":   event.Payload,
		})
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
	}

	return *withTimestamp(NewStateSnapshotEvent(map[string]any{
		"threadId":        sessionModel.SessionID,
		"sessionId":       sessionModel.SessionID,
		"title":           sessionModel.Title,
		"status":          string(sessionModel.Status),
		"turnId":          sessionModel.LastTurnID,
		"lastTurnId":      sessionModel.LastTurnID,
		"currentRunId":    currentRunID,
		"currentAgentId":  currentAgentID,
		"latestEventType": latestEventType,
		"eventCount":      len(mappedEvents),
		"externalTasks":   mappedExternalTasks,
		"events":          mappedEvents,
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
