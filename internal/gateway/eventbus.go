// Package gateway provides the HTTP server, SSE event streaming, and REST API.
package gateway

import (
	"sync"
)

// Event is a generic event published to the bus.
type Event struct {
	Type    string `json:"type"`
	TraceID string `json:"trace_id,omitempty"`
	AgentID string `json:"agent_id,omitempty"`
	Payload any    `json:"payload,omitempty"`
}

// EventBus is a pub/sub bus for streaming events to SSE clients.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[string][]chan Event // keyed by traceID or "*" for wildcard
}

// NewEventBus creates a new EventBus.
func NewEventBus() *EventBus {
	return &EventBus{subscribers: make(map[string][]chan Event)}
}

// Subscribe returns a channel that receives events for the given traceID.
// Use "*" to receive all events.
func (b *EventBus) Subscribe(traceID string) <-chan Event {
	ch := make(chan Event, 64)
	b.mu.Lock()
	b.subscribers[traceID] = append(b.subscribers[traceID], ch)
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel. The caller must pass the same channel
// returned by Subscribe.
func (b *EventBus) Unsubscribe(traceID string, ch <-chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	chans := b.subscribers[traceID]
	for i, c := range chans {
		if c == ch {
			b.subscribers[traceID] = append(chans[:i], chans[i+1:]...)
			close(c)
			return
		}
	}
}

// Publish sends ev to all subscribers matching ev.TraceID and all wildcard subscribers.
func (b *EventBus) Publish(ev Event) {
	b.mu.RLock()
	targets := make([]chan Event, 0)
	targets = append(targets, b.subscribers[ev.TraceID]...)
	targets = append(targets, b.subscribers["*"]...)
	b.mu.RUnlock()

	for _, ch := range targets {
		select {
		case ch <- ev:
		default:
			// Drop if slow subscriber; avoid blocking.
		}
	}
}
