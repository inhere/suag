package gateway

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/inhere/axon/internal/host/session"
)

type wsCommandRunner interface {
	ContinueTurn(ctx context.Context, in session.ContinueTurnInput) (session.StartTurnResult, error)
}

type wsCommand struct {
	Type          string `json:"type"`
	SessionID     string `json:"session_id"`
	Message       string `json:"message"`
	AgentID       string `json:"agent_id"`
	InitiatorType string `json:"initiator_type"`
}

func WSHandler(bus *EventBus, sessionSvc wsCommandRunner) http.HandlerFunc {
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
		ch := bus.Subscribe(sub)
		defer bus.Unsubscribe(sub, ch)

		if err := wsjson.Write(ctx, conn, map[string]any{
			"type":       "connected",
			"session_id": sub.SessionID,
			"trace_id":   sub.TraceID,
			"wildcard":   sub.Wildcard,
		}); err != nil {
			return
		}

		go func() {
			for {
				var cmd wsCommand
				if err := wsjson.Read(ctx, conn, &cmd); err != nil {
					return
				}
				if err := handleWSCommand(ctx, conn, sub, sessionSvc, cmd); err != nil {
					_ = wsjson.Write(ctx, conn, map[string]any{
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
				if err := wsjson.Write(ctx, conn, ev); err != nil {
					return
				}
			}
		}
	}
}

func handleWSCommand(ctx context.Context, conn *websocket.Conn, sub Subscription, sessionSvc wsCommandRunner, cmd wsCommand) error {
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

		result, err := sessionSvc.ContinueTurn(ctx, session.ContinueTurnInput{
			SessionID:     sessionID,
			AgentID:       cmd.AgentID,
			Message:       cmd.Message,
			InitiatorType: session.InitiatorType(cmd.InitiatorType),
		})
		if err != nil {
			return err
		}
		return wsjson.Write(ctx, conn, map[string]any{
			"type":       "command.accepted",
			"command":    "continue_turn",
			"session_id": result.SessionID,
			"turn_id":    result.TurnID,
			"run_id":     result.RunID,
		})
	default:
		return nil
	}
}
