package gateway

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

// SSEHandler streams events from the EventBus to an HTTP client via Server-Sent Events.
// Clients connect to GET /events?trace_id=<id> (or trace_id=* for all events).
func SSEHandler(bus *EventBus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Verify the client supports flushing.
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		traceID := r.URL.Query().Get("trace_id")
		if traceID == "" {
			traceID = "*"
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		ch := bus.Subscribe(traceID)
		defer bus.Unsubscribe(traceID, ch)

		// Send an initial "connected" event.
		writeSSE(w, flusher, "connected", map[string]string{"trace_id": traceID})

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

func writeSSE(w http.ResponseWriter, f http.Flusher, event string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		slog.Warn("sse marshal error", "err", err)
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(b))
	f.Flush()
}
