package agui

import (
	"time"

	"github.com/inhere/axon/internal/host/session"
)

func MapEvent(event HostEvent) []any {
	now := time.Now().UnixMilli()

	switch event.Type {
	case session.EventRunStarted:
		return []any{
			RunStartedEvent{
				Type:      EventTypeRunStarted,
				Timestamp: now,
				ThreadID:  event.SessionID,
				RunID:     event.RunID,
			},
			buildRunActivitySnapshot(now, event, "running"),
		}
	case session.EventRunFinished:
		textEvents := buildFinishedTextEvents(now, event)
		out := []any{
			RunFinishedEvent{
				Type:      EventTypeRunFinished,
				Timestamp: now,
				ThreadID:  event.SessionID,
				RunID:     event.RunID,
				Result:    event.Payload,
			},
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
		return []any{
			RunErrorEvent{
				Type:      EventTypeRunError,
				Timestamp: now,
				RunID:     event.RunID,
				Message:   message,
			},
			buildRunActivitySnapshot(now, event, "failed"),
		}
	}

	name := "axon.event"
	if event.Type == session.EventRunQueued {
		name = "axon.run.queued"
	}

	return []any{
		CustomEvent{
			Type:      EventTypeCustom,
			Timestamp: now,
			Name:      name,
			Value: map[string]any{
				"threadId":  event.SessionID,
				"sessionId": event.SessionID,
				"turnId":    event.TurnID,
				"runId":     event.RunID,
				"agentId":   event.AgentID,
				"sequence":  event.Sequence,
				"type":      event.Type,
				"payload":   event.Payload,
			},
		},
		buildRunActivitySnapshot(now, event, "queued"),
	}
}

func buildFinishedTextEvents(now int64, event HostEvent) []any {
	payload, ok := event.Payload.(map[string]any)
	if !ok {
		return nil
	}

	summary, ok := payload["summary"].(string)
	if !ok || summary == "" {
		return nil
	}

	messageID := event.RunID + ":assistant"
	return []any{
		TextMessageStartEvent{
			Type:      EventTypeTextMessageStart,
			Timestamp: now,
			MessageID: messageID,
			Role:      "assistant",
		},
		TextMessageContentEvent{
			Type:      EventTypeTextMessageContent,
			Timestamp: now,
			MessageID: messageID,
			Delta:     summary,
		},
		TextMessageEndEvent{
			Type:      EventTypeTextMessageEnd,
			Timestamp: now,
			MessageID: messageID,
		},
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

	return StateSnapshotEvent{
		Type:      EventTypeStateSnapshot,
		Timestamp: now,
		Snapshot: map[string]any{
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
		},
	}
}

func buildRunActivitySnapshot(now int64, event HostEvent, status string) ActivitySnapshotEvent {
	return ActivitySnapshotEvent{
		Type:         EventTypeActivitySnapshot,
		Timestamp:    now,
		MessageID:    event.RunID,
		ActivityType: "run_status",
		Content: map[string]any{
			"threadId": event.SessionID,
			"turnId":   event.TurnID,
			"runId":    event.RunID,
			"agentId":  event.AgentID,
			"status":   status,
		},
	}
}
