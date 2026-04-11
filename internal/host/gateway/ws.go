package gateway

import (
	"log/slog"
	"net/http"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func WSHandler(bus *EventBus) http.HandlerFunc {
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
