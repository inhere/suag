package gateway

import "testing"

func TestEventBus_SubscribeSessionReceivesMatchingEventsOnly(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	sub := Subscription{SessionID: "sess_001"}
	ch := bus.Subscribe(sub)
	defer bus.Unsubscribe(sub, ch)

	bus.Publish(Event{Type: "run.queued", SessionID: "sess_002"})
	bus.Publish(Event{Type: "run.started", SessionID: "sess_001", TurnID: "turn_001", RunID: "run_001"})

	select {
	case ev := <-ch:
		if ev.Type != "run.started" {
			t.Fatalf("event type = %q, want %q", ev.Type, "run.started")
		}
		if ev.SessionID != "sess_001" {
			t.Fatalf("event session_id = %q, want %q", ev.SessionID, "sess_001")
		}
	default:
		t.Fatal("expected matching session event")
	}
}

func TestEventBus_WildcardReceivesAllEvents(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	sub := Subscription{Wildcard: true}
	ch := bus.Subscribe(sub)
	defer bus.Unsubscribe(sub, ch)

	bus.Publish(Event{Type: "run.queued", SessionID: "sess_001"})
	bus.Publish(Event{Type: "span", TraceID: "trace_001"})

	for i := 0; i < 2; i++ {
		select {
		case <-ch:
		default:
			t.Fatalf("expected wildcard event #%d", i+1)
		}
	}
}

func TestEventBus_SubscribeTraceReceivesLegacyTraceEvents(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	sub := Subscription{TraceID: "trace_001"}
	ch := bus.Subscribe(sub)
	defer bus.Unsubscribe(sub, ch)

	bus.Publish(Event{Type: "span", TraceID: "trace_002"})
	bus.Publish(Event{Type: "span", TraceID: "trace_001", AgentID: "agent_001"})

	select {
	case ev := <-ch:
		if ev.TraceID != "trace_001" {
			t.Fatalf("event trace_id = %q, want %q", ev.TraceID, "trace_001")
		}
	default:
		t.Fatal("expected matching trace event")
	}
}
