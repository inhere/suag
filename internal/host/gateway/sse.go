package gateway

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

// SSEHandler streams events from the EventBus to an HTTP client via Server-Sent Events.
// Clients connect to GET /events?session_id=<id>, GET /events?trace_id=<id>,
// or use * to subscribe to all events during the transition period.
func SSEHandler(bus *EventBus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Verify the client supports flushing.
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		sub := readSSESubscription(r)

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		ch := bus.Subscribe(sub)
		defer bus.Unsubscribe(sub, ch)

		// Send an initial "connected" event.
		writeSSE(w, flusher, "connected", sseConnectedPayload(sub))

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				writeSSE(w, flusher, ev.Type, ev)
			}
		}
	}
}

func readSSESubscription(r *http.Request) Subscription {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "*" {
		return Subscription{Wildcard: true}
	}
	if sessionID != "" {
		return Subscription{SessionID: sessionID}
	}

	traceID := r.URL.Query().Get("trace_id")
	if traceID == "*" || traceID == "" {
		return Subscription{Wildcard: true}
	}
	return Subscription{TraceID: traceID}
}

func sseConnectedPayload(sub Subscription) map[string]any {
	payload := map[string]any{
		"wildcard": sub.Wildcard,
	}
	if sub.SessionID != "" {
		payload["session_id"] = sub.SessionID
	}
	if sub.TraceID != "" {
		payload["trace_id"] = sub.TraceID
	}
	return payload
}

func writeSSE(w http.ResponseWriter, f http.Flusher, event string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		slog.Warn("sse marshal error", "err", err)
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(b))
	f.Flush()
}
