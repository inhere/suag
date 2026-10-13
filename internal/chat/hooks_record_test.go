package chat

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"

	suaghooks "github.com/inhere/suag/internal/hooks"
	"github.com/inhere/suag/internal/personal"
)

type eventSink struct {
	mu     sync.Mutex
	events []personal.HostEvent
}

func (s *eventSink) RecordEvent(_ context.Context, _ string, ev personal.HostEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
}

func (s *eventSink) all() []personal.HostEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]personal.HostEvent(nil), s.events...)
}

// stubHooks answers Call with resp and StartStop with a wait on done.
type stubHooks struct {
	resp suaghooks.Response
	done chan suaghooks.Result
}

func (s *stubHooks) Call(context.Context, suaghooks.Event) suaghooks.Response { return s.resp }
func (s *stubHooks) StartStop(context.Context, suaghooks.Event) suaghooks.Wait {
	return &fakeStopWait{done: s.done, interrupt: func() {}}
}
func (s *stubHooks) PostToolName(tool string) (string, bool) { return tool, true }

func TestRecordingHooks(t *testing.T) {
	sink := &eventSink{}
	stub := &stubHooks{resp: suaghooks.Response{Notice: "taken over"}, done: make(chan suaghooks.Result, 1)}
	h := &recordingHooks{Client: stub, runner: sink}

	h.Call(context.Background(), suaghooks.Event{Name: "UserPromptSubmit", SessionID: "s1"})
	stub.done <- suaghooks.Result{Response: suaghooks.Response{Decision: "block", Reason: "go on"}}
	w := h.StartStop(context.Background(), suaghooks.Event{Name: "Stop", SessionID: "s1"})
	r := <-w.Done() // passed on unchanged
	assert.Eq(t, "go on", r.Response.Reason)
	h.Call(context.Background(), suaghooks.Event{Name: "SessionEnd"}) // no session: not recorded

	events := sink.all()
	assert.Require(t, assert.Len(t, events, 2))
	assert.Eq(t, "UserPromptSubmit", events[0].Name)
	assert.Eq(t, map[string]any{"notice": true}, events[0].Attrs)
	assert.Eq(t, "Stop", events[1].Name)
	assert.Eq(t, map[string]any{"decision": "block", "reason_chars": 5}, events[1].Attrs)

	t.Run("an interrupt hands back a reply nobody read", func(t *testing.T) {
		stub.done = make(chan suaghooks.Result, 1)
		stub.done <- suaghooks.Result{Response: suaghooks.Response{Decision: "block", Reason: "pending"}}
		w := h.StartStop(context.Background(), suaghooks.Event{Name: "Stop", SessionID: "s1"})
		time.Sleep(20 * time.Millisecond) // the forwarder took it
		assert.Eq(t, "pending", w.Interrupt(context.Background()))
	})
}
