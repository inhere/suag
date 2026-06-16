// Package gateway provides the HTTP server, SSE event streaming, and REST API.
package gateway

import (
	"context"
	"strings"
	"sync"

	"github.com/inhere/suag/internal/host/session"
)

// Event is a generic event published to the bus.
type Event struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`
	TurnID    string `json:"turn_id,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	TraceID   string `json:"trace_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	Sequence  int64  `json:"sequence,omitempty"`
	Payload   any    `json:"payload,omitempty"`
}

type Subscription struct {
	SessionID string
	TraceID   string
	Wildcard  bool
}

// EventBus is a pub/sub bus for streaming events to SSE clients.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[string][]chan Event
}

// NewEventBus creates a new EventBus.
func NewEventBus() *EventBus {
	return &EventBus{subscribers: make(map[string][]chan Event)}
}

// Subscribe returns a channel that receives events for the given subscription.
func (b *EventBus) Subscribe(sub Subscription) <-chan Event {
	ch := make(chan Event, 64)
	key := subscriptionKey(sub)
	b.mu.Lock()
	b.subscribers[key] = append(b.subscribers[key], ch)
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel. The caller must pass the same channel
// returned by Subscribe.
func (b *EventBus) Unsubscribe(sub Subscription, ch <-chan Event) {
	key := subscriptionKey(sub)
	b.mu.Lock()
	defer b.mu.Unlock()
	chans := b.subscribers[key]
	for i, c := range chans {
		if c == ch {
			b.subscribers[key] = append(chans[:i], chans[i+1:]...)
			close(c)
			return
		}
	}
}

// Publish sends ev to all subscribers matching the session, trace, and wildcard routes.
func (b *EventBus) Publish(ev Event) {
	b.mu.RLock()
	targets := make([]chan Event, 0)
	if key := sessionSubscriptionKey(ev.SessionID); key != "" {
		targets = append(targets, b.subscribers[key]...)
	}
	if key := traceSubscriptionKey(ev.TraceID); key != "" {
		targets = append(targets, b.subscribers[key]...)
	}
	targets = append(targets, b.subscribers[wildcardSubscriptionKey()]...)
	b.mu.RUnlock()

	for _, ch := range targets {
		select {
		case ch <- ev:
		default:
			// Drop if slow subscriber; avoid blocking.
		}
	}
}

func (b *EventBus) PublishSessionEvent(_ context.Context, event session.RunEvent) error {
	b.Publish(Event{
		Type:      event.EventType,
		SessionID: event.SessionID,
		TurnID:    event.TurnID,
		RunID:     event.RunID,
		AgentID:   event.AgentID,
		Sequence:  event.Sequence,
		Payload:   event.Payload,
	})
	return nil
}

func subscriptionKey(sub Subscription) string {
	if sub.Wildcard {
		return wildcardSubscriptionKey()
	}
	if value := strings.TrimSpace(sub.SessionID); value != "" {
		return sessionSubscriptionKey(value)
	}
	if value := strings.TrimSpace(sub.TraceID); value != "" {
		return traceSubscriptionKey(value)
	}
	return wildcardSubscriptionKey()
}

func sessionSubscriptionKey(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	return "session:" + sessionID
}

func traceSubscriptionKey(traceID string) string {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return ""
	}
	return "trace:" + traceID
}

func wildcardSubscriptionKey() string {
	return "*"
}
