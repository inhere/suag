package gateway

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/inhere/axon/internal/host/agui"
	"github.com/inhere/axon/internal/host/session"
)

type wsCommandRunner interface {
	ContinueTurn(ctx context.Context, in session.ContinueTurnInput) (session.StartTurnResult, error)
}

type wsSessionStore interface {
	GetSession(ctx context.Context, sessionID string) (session.Session, error)
	ListEventsBySession(ctx context.Context, sessionID string, afterSequence int64) ([]session.RunEvent, error)
}

type wsCommand struct {
	Type          string `json:"type"`
	SessionID     string `json:"session_id"`
	Message       string `json:"message"`
	AgentID       string `json:"agent_id"`
	InitiatorType string `json:"initiator_type"`
}

func WSHandler(bus *EventBus, sessionStore wsSessionStore, sessionSvc wsCommandRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sub := readSSESubscription(r)
		if !sub.Wildcard && sub.SessionID == "" && sub.TraceID == "" {
			httpError(w, "session_id or trace_id is required", http.StatusBadRequest)
			return
		}

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			slog.Warn("websocket accept error", "err", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "closed")

		ctx := r.Context()
		var writeMu sync.Mutex
		ch := bus.Subscribe(sub)
		defer bus.Unsubscribe(sub, ch)

		if err := writeWSJSON(ctx, conn, &writeMu, map[string]any{
			"type":       "connected",
			"session_id": sub.SessionID,
			"trace_id":   sub.TraceID,
			"wildcard":   sub.Wildcard,
		}); err != nil {
			return
		}

		if sub.SessionID != "" && sessionStore != nil {
			snapshot, err := buildWSStateSnapshot(ctx, sessionStore, sub.SessionID)
			if err == nil {
				if err := writeWSJSON(ctx, conn, &writeMu, snapshot); err != nil {
					return
				}
			}
		}

		go func() {
			for {
				var cmd wsCommand
				if err := wsjson.Read(ctx, conn, &cmd); err != nil {
					return
				}
				if err := handleWSCommand(ctx, conn, &writeMu, sub, sessionSvc, cmd); err != nil {
					_ = writeWSJSON(ctx, conn, &writeMu, map[string]any{
						"type":  "command.error",
						"error": err.Error(),
					})
				}
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if err := wsjson.Write(ctx, conn, agui.MapEvent(agui.HostEvent{
					Type:      ev.Type,
					SessionID: ev.SessionID,
					TurnID:    ev.TurnID,
					RunID:     ev.RunID,
					AgentID:   ev.AgentID,
					Sequence:  ev.Sequence,
					Payload:   ev.Payload,
				})); err != nil {
					return
				}
			}
		}
	}
}

func buildWSStateSnapshot(ctx context.Context, store wsSessionStore, sessionID string) (agui.StateSnapshotEvent, error) {
	sessionModel, err := store.GetSession(ctx, sessionID)
	if err != nil {
		return agui.StateSnapshotEvent{}, err
	}

	runEvents, err := store.ListEventsBySession(ctx, sessionID, 0)
	if err != nil {
		return agui.StateSnapshotEvent{}, err
	}

	events := make([]agui.HostEvent, 0, len(runEvents))
	for _, event := range runEvents {
		events = append(events, agui.HostEvent{
			Type:      event.EventType,
			SessionID: event.SessionID,
			TurnID:    event.TurnID,
			RunID:     event.RunID,
			AgentID:   event.AgentID,
			Sequence:  event.Sequence,
			Payload:   event.Payload,
		})
	}

	return agui.BuildStateSnapshot(sessionModel, events), nil
}

func handleWSCommand(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex, sub Subscription, sessionSvc wsCommandRunner, cmd wsCommand) error {
	switch strings.TrimSpace(cmd.Type) {
	case "", "subscribe":
		return nil
	case "continue_turn":
		if sessionSvc == nil {
			return http.ErrNotSupported
		}

		sessionID := strings.TrimSpace(cmd.SessionID)
		if sessionID == "" {
			sessionID = sub.SessionID
		}

		if err := writeWSJSON(ctx, conn, writeMu, map[string]any{
			"type":       "command.accepted",
			"command":    "continue_turn",
			"session_id": sessionID,
		}); err != nil {
			return err
		}

		_, err := sessionSvc.ContinueTurn(ctx, session.ContinueTurnInput{
			SessionID:     sessionID,
			AgentID:       cmd.AgentID,
			Message:       cmd.Message,
			InitiatorType: session.InitiatorType(cmd.InitiatorType),
		})
		if err != nil {
			return err
		}
		return nil
	default:
		return nil
	}
}

func writeWSJSON(ctx context.Context, conn *websocket.Conn, mu *sync.Mutex, payload any) error {
	mu.Lock()
	defer mu.Unlock()
	return wsjson.Write(ctx, conn, payload)
}
