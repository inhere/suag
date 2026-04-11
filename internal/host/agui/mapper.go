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
		return []any{
			RunFinishedEvent{
				Type:      EventTypeRunFinished,
				Timestamp: now,
				ThreadID:  event.SessionID,
				RunID:     event.RunID,
				Result:    event.Payload,
			},
			buildRunActivitySnapshot(now, event, "completed"),
		}
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

func BuildStateSnapshot(sessionModel session.Session, events []HostEvent) StateSnapshotEvent {
	now := time.Now().UnixMilli()

	mappedEvents := make([]map[string]any, 0, len(events))
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
