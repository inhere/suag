package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/inhere/axon/internal/host/agui"
	"github.com/inhere/axon/internal/host/session"
)

var wsAcceptOptions = &websocket.AcceptOptions{
	OriginPatterns: []string{
		"localhost:*",
		"127.0.0.1:*",
		"[::1]:*",
	},
}

type wsCommandRunner interface {
	ContinueTurn(ctx context.Context, in session.ContinueTurnInput) (session.StartTurnResult, error)
	StopTurn(ctx context.Context, in session.StopTurnInput) error
}

type wsSessionStore interface {
	GetSession(ctx context.Context, sessionID string) (session.Session, error)
	ListEventsBySession(ctx context.Context, sessionID string, afterSequence int64) ([]session.RunEvent, error)
	ListExternalTasksBySession(ctx context.Context, sessionID string) ([]session.ExternalTask, error)
}

type wsCommand struct {
	Type          string `json:"type"`
	SessionID     string `json:"session_id"`
	TurnID        string `json:"turn_id"`
	Message       string `json:"message"`
	AgentID       string `json:"agent_id"`
	InitiatorType string `json:"initiator_type"`
	Reason        string `json:"reason"`
}

func WSHandler(bus *EventBus, sessionStore wsSessionStore, sessionSvc wsCommandRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sub := readSSESubscription(r)
		if !sub.Wildcard && sub.SessionID == "" && sub.TraceID == "" {
			httpError(w, "session_id or trace_id is required", http.StatusBadRequest)
			return
		}

		conn, err := websocket.Accept(w, r, wsAcceptOptions)
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
				go func(command wsCommand) {
					writeMu.Lock()
					defer writeMu.Unlock()

					response, err := handleWSCommand(ctx, sub, sessionSvc, command)
					if err != nil {
						_ = wsjson.Write(ctx, conn, map[string]any{
							"type":  "command.error",
							"error": err.Error(),
						})
						return
					}
					if response == nil {
						return
					}
					_ = wsjson.Write(ctx, conn, response)
				}(cmd)
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
				mappedEvents := agui.MapEvent(agui.HostEvent{
					Type:      ev.Type,
					SessionID: ev.SessionID,
					TurnID:    ev.TurnID,
					RunID:     ev.RunID,
					AgentID:   ev.AgentID,
					Sequence:  ev.Sequence,
					Payload:   ev.Payload,
				})
				for _, mapped := range mappedEvents {
					if err := writeWSJSON(ctx, conn, &writeMu, mapped); err != nil {
						return
					}
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
	externalTasks, err := store.ListExternalTasksBySession(ctx, sessionID)
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
			CreatedAt: event.CreatedAt,
		})
	}

	return agui.BuildStateSnapshot(sessionModel, events, externalTasks), nil
}

func handleWSCommand(ctx context.Context, sub Subscription, sessionSvc wsCommandRunner, cmd wsCommand) (map[string]any, error) {
	switch strings.TrimSpace(cmd.Type) {
	case "", "subscribe":
		return nil, nil
	case "continue_turn":
		if sessionSvc == nil {
			return nil, http.ErrNotSupported
		}

		sessionID := strings.TrimSpace(cmd.SessionID)
		if sessionID == "" {
			sessionID = sub.SessionID
		}

		_, err := sessionSvc.ContinueTurn(ctx, session.ContinueTurnInput{
			SessionID:     sessionID,
			AgentID:       cmd.AgentID,
			Message:       cmd.Message,
			InitiatorType: session.InitiatorType(cmd.InitiatorType),
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"type":       "command.accepted",
			"command":    "continue_turn",
			"session_id": sessionID,
		}, nil
	case "stop_turn":
		if sessionSvc == nil {
			return nil, http.ErrNotSupported
		}

		sessionID := strings.TrimSpace(cmd.SessionID)
		if sessionID == "" {
			sessionID = sub.SessionID
		}
		turnID := strings.TrimSpace(cmd.TurnID)
		if turnID == "" {
			return nil, fmt.Errorf("turn id is required")
		}

		if err := sessionSvc.StopTurn(ctx, session.StopTurnInput{
			SessionID: sessionID,
			TurnID:    turnID,
			Reason:    cmd.Reason,
		}); err != nil {
			return nil, err
		}

		return map[string]any{
			"type":       "command.accepted",
			"command":    "stop_turn",
			"session_id": sessionID,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported websocket command: %s", strings.TrimSpace(cmd.Type))
	}
}

func writeWSJSON(ctx context.Context, conn *websocket.Conn, mu *sync.Mutex, payload any) error {
	mu.Lock()
	defer mu.Unlock()
	return wsjson.Write(ctx, conn, payload)
}
