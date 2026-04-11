package agui

import (
	"time"

	"github.com/inhere/axon/internal/host/session"
)

func MapEvent(event HostEvent) CustomEvent {
	now := time.Now().UnixMilli()

	name := "axon.event"
	if event.Type == "run.queued" {
		name = "axon.run.queued"
	}

	return CustomEvent{
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
	}
}

func BuildStateSnapshot(sessionModel session.Session, events []HostEvent) StateSnapshotEvent {
	now := time.Now().UnixMilli()

	mappedEvents := make([]map[string]any, 0, len(events))
	for _, event := range events {
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
			"threadId":   sessionModel.SessionID,
			"sessionId":  sessionModel.SessionID,
			"title":      sessionModel.Title,
			"status":     string(sessionModel.Status),
			"turnId":     sessionModel.LastTurnID,
			"lastTurnId": sessionModel.LastTurnID,
			"events":     mappedEvents,
		},
	}
}
