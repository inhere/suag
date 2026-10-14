package personal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/tools"
)

// Tests for the session steering inbox: a person types into a running turn, the
// turn's next model request (but not a wrap-up step) carries the message, and
// whatever the turn did not deliver comes back in RunResult.ReturnedSteers.

// eventRecorder is a turn's Emit sink: it keeps the events a turn emits and can
// run an extra step (steer, cancel) from inside one of them. The callback runs
// on the turn's own goroutine, so it may touch the test's runner directly.
type eventRecorder struct {
	events []Event
	on     func(Event)
}

func (r *eventRecorder) emit(_ context.Context, ev Event) error {
	r.events = append(r.events, ev)
	if r.on != nil {
		r.on(ev)
	}
	return nil
}

// kinds returns the recorded events of one kind.
func (r *eventRecorder) kinds(kind string) []Event {
	var out []Event
	for _, ev := range r.events {
		if ev.Kind == kind {
			out = append(out, ev)
		}
	}
	return out
}

// steerEventsOf returns the steering trace events of a session with one name.
func steerEventsOf(t *testing.T, repo *store.Store, sessionID, name string) []store.Event {
	t.Helper()
	var out []store.Event
	for _, e := range eventsOf(t, repo, sessionID, store.EventSteer) {
		if e.Name == name {
			out = append(out, e)
		}
	}
	return out
}

// workCall asks the model for one call of the "work" tool.
func workCall(id string) (*agentcore.GenerateResult, error) {
	return &agentcore.GenerateResult{
		ToolCalls:    []agentcore.ToolCall{{CallID: id, Name: "work", Arguments: map[string]any{}}},
		FinishReason: agentcore.FinishToolCalls,
	}, nil
}

// workRegistry is a registry holding the "work" tool whose body is work.
func workRegistry(work func()) agentcore.ToolRegistry {
	registry := tools.NewRegistry()
	registry.Register(fnTool{name: "work", fn: func() (*agentcore.ToolResult, error) {
		work()
		return &agentcore.ToolResult{Content: "worked"}, nil
	}})
	return registry
}

// steerIDs lists the ids of steers.
func steerIDs(steers []Steer) []string {
	out := make([]string, len(steers))
	for i, s := range steers {
		out[i] = s.ID
	}
	return out
}

// turnCounter counts the turn-level hook runs of a runner: a steer is not a
// turn of its own, so it must not produce another run.
type turnCounter struct {
	agentcore.NopHook
	runs int
}

func (c *turnCounter) BeforeRun(context.Context, *agentcore.RunEvent, *agentcore.Control) error {
	c.runs++
	return nil
}

// The inbox itself: what it accepts, what it takes and what it hands back.
func TestSteerInboxRules(t *testing.T) {
	t.Run("an empty steer is refused", func(t *testing.T) {
		in := &steerInbox{}
		accepted, err := in.push(Steer{ID: "s1", Text: " \n "})
		assert.NoErr(t, err)
		assert.Eq(t, false, accepted)
		assert.Len(t, in.pending, 0)
	})

	t.Run("a full inbox is refused", func(t *testing.T) {
		in := &steerInbox{}
		for i := 0; i < steerCapacity; i++ {
			accepted, err := in.push(Steer{ID: fmt.Sprintf("s%d", i), Text: "go on"})
			assert.NoErr(t, err)
			assert.Eq(t, true, accepted)
		}
		accepted, err := in.push(Steer{ID: "overflow", Text: "go on"})
		assert.Eq(t, false, accepted)
		assert.ErrIs(t, err, ErrSteerQueueFull)
	})

	t.Run("a closed inbox is refused", func(t *testing.T) {
		in := &steerInbox{}
		in.close()
		accepted, err := in.push(Steer{ID: "s1", Text: "go on"})
		assert.NoErr(t, err)
		assert.Eq(t, false, accepted)
		assert.Empty(t, in.take())
	})

	t.Run("a completed turn returns the untaken ones", func(t *testing.T) {
		in := &steerInbox{}
		_, _ = in.push(Steer{ID: "s1", Text: "first"})
		_, _ = in.push(Steer{ID: "s2", Text: "second"})
		assert.Len(t, in.take(), 2) // both entered a request
		_, _ = in.push(Steer{ID: "s3", Text: "third"})
		got := in.returned(true)
		assert.Eq(t, []string{"s3"}, steerIDs(got)) // s1, s2 stay in the turn's history
		assert.Empty(t, in.returned(true))
	})

	t.Run("a cancelled turn returns the taken ones first", func(t *testing.T) {
		in := &steerInbox{}
		_, _ = in.push(Steer{ID: "s1", Text: "first"})
		_, _ = in.push(Steer{ID: "s2", Text: "second"})
		assert.Len(t, in.take(), 2)
		_, _ = in.push(Steer{ID: "s3", Text: "third"})
		assert.Eq(t, []string{"s1", "s2", "s3"}, steerIDs(in.returned(false)))
	})
}

func TestSteerIsDeliveredToTheRunningTurn(t *testing.T) {
	repo := openObserveRepo(t)
	const sid = "s-steer-turn"
	steers := []Steer{
		{ID: "s1", Text: "only change the README", Source: steerSourceKeyboard},
		{ID: "s2", Text: "and run the tests", Source: "send:alice"},
	}
	var runner *Runner
	hooks := &turnCounter{}
	provider := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		if n == 1 {
			return workCall("c1")
		}
		return finalText("done")
	}}
	runner = compactRunner(t, repo, provider, workRegistry(func() {
		// Both arrive while the tool runs: the next request delivers both (D6).
		for _, s := range steers {
			accepted, err := runner.Steer(sid, s)
			assert.NoErr(t, err)
			assert.Eq(t, true, accepted)
		}
	}), func(c *Config) { c.Hooks = []agentcore.Hook{hooks} })
	_, err := runner.NewSession(context.Background(), sid, "steer test")
	assert.Require(t, assert.NoErr(t, err))
	rec := &eventRecorder{}

	start := time.Now()
	res, err := runner.Run(context.Background(), RunRequest{SessionID: sid, Prompt: "fix the parser", Emit: rec.emit})
	t.Logf("turn took %s", time.Since(start))
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, "done", res.Output)
	assert.Empty(t, res.ReturnedSteers)

	t.Run("both are in the next request, in order, after the tool result", func(t *testing.T) {
		assert.Require(t, assert.Eq(t, 2, len(provider.reqs)))
		assert.Eq(t, -1, indexOf(provider.reqs[0].Messages, steers[0].Text))
		second := provider.reqs[1].Messages
		first, last := indexOf(second, steers[0].Text), indexOf(second, steers[1].Text)
		assert.Require(t, assert.True(t, first > 0))
		assert.Eq(t, first+1, last)
		assert.Eq(t, indexOf(second, "worked"), first-1)
		assert.Eq(t, "user", second[first].Role)
		assert.Eq(t, "user", second[last].Role)
	})

	t.Run("each is a stored entry of the turn", func(t *testing.T) {
		entries, err := repo.ListEntries(context.Background(), sid,
			store.EntryFilter{TurnID: res.TurnID, Types: []string{store.EntryUser}})
		assert.Require(t, assert.NoErr(t, err))
		var got []Steer
		for _, e := range entries {
			var m agentcore.Message
			assert.Require(t, assert.NoErr(t, json.Unmarshal(e.JSON, &m)))
			if !isSteerItem(m) {
				continue
			}
			assert.Eq(t, "user", m.Role)
			got = append(got, Steer{ID: m.Metadata[metaSteerID].(string), Text: m.Content,
				Source: m.Metadata[metaSteerSource].(string)})
		}
		assert.Eq(t, steers, got)
	})

	t.Run("delivery is a trace event and an emitted event", func(t *testing.T) {
		events := steerEventsOf(t, repo, sid, "delivered")
		assert.Require(t, assert.Len(t, events, 2))
		assert.Eq(t, "s1", events[0].Attrs["steer_id"])
		assert.Eq(t, steerSourceKeyboard, events[0].Attrs["source"])
		assert.Eq(t, float64(len(steers[0].Text)), events[0].Attrs["chars"])
		assert.Eq(t, float64(1), events[0].Attrs["step"]) // taken by the step after the tool call
		assert.Eq(t, res.TurnID, events[0].TurnID)
		emitted := rec.kinds("steer_delivered")
		assert.Require(t, assert.Len(t, emitted, 2))
		assert.Eq(t, "s2", emitted[1].SteerID)
		assert.Eq(t, "send:alice", emitted[1].SteerSource)
		assert.Eq(t, steers[1].Text, emitted[1].Text)
		assert.Empty(t, rec.kinds("steer_returned"))
	})

	t.Run("a steer is no turn of its own", func(t *testing.T) {
		// The turn's hooks ran once: the steers did not start anything, and no
		// UserPromptSubmit of a new turn could have seen them.
		assert.Eq(t, 1, hooks.runs)
	})
}

// A wrap-up step only asks for a final answer: the steers wait for the turn's
// end and come back to the caller (design D5).
func TestSteerIsNotTakenOnAWrapUpStep(t *testing.T) {
	repo := openObserveRepo(t)
	const sid = "s-steer-wrapup"
	steer := Steer{ID: "s1", Text: "stop and explain", Source: steerSourceKeyboard}
	var runner *Runner
	provider := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		if n == 1 {
			return workCall("c1")
		}
		return finalText("answered from what I have")
	}}
	// The step limit makes the request after the tool call the wrap-up step.
	runner = compactRunner(t, repo, provider, workRegistry(func() {
		accepted, err := runner.Steer(sid, steer)
		assert.NoErr(t, err)
		assert.Eq(t, true, accepted)
	}), func(c *Config) { c.MaxSteps = 1 })
	_, err := runner.NewSession(context.Background(), sid, "steer wrap-up")
	assert.Require(t, assert.NoErr(t, err))
	rec := &eventRecorder{}

	res, err := runner.Run(context.Background(), RunRequest{SessionID: sid, Prompt: "run the job", Emit: rec.emit})
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, "max_steps", res.WrapUp)
	assert.Eq(t, []Steer{steer}, res.ReturnedSteers)

	t.Run("the wrap-up request did not carry it", func(t *testing.T) {
		assert.Require(t, assert.Eq(t, 2, len(provider.reqs)))
		assert.Eq(t, -1, indexOf(provider.reqs[1].Messages, steer.Text))
		assert.Empty(t, steerEventsOf(t, repo, sid, "delivered"))
	})
	t.Run("it comes back with the reason and disposition of a next turn", func(t *testing.T) {
		returned := steerEventsOf(t, repo, sid, "returned")
		assert.Require(t, assert.Len(t, returned, 1))
		assert.Eq(t, "completed_untaken", returned[0].Attrs["reason"])
		assert.Eq(t, "next_turn", returned[0].Attrs["disposition"])
		assert.Eq(t, "s1", returned[0].Attrs["steer_id"])
		emitted := rec.kinds("steer_returned")
		assert.Require(t, assert.Len(t, emitted, 1))
		assert.Eq(t, steer.Text, emitted[0].Text)
	})
}

// abortProv answers the first model call with a tool call and hands the context
// error back for every later one: the provider of a turn aborted between its
// context build and the request going out.
type abortProv struct {
	mu   sync.Mutex
	reqs []*agentcore.GenerateRequest
}

func (p *abortProv) Name() string { return "abort" }

func (p *abortProv) Generate(ctx context.Context, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	n := len(p.reqs)
	p.mu.Unlock()
	if n == 1 {
		return workCall("c1")
	}
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
	}
	return nil, ctx.Err()
}

// A turn cancelled between BuildMessages and its model request returns every
// steer: the delivered one (its entry does not replay either) and the ones
// still in the inbox (design D4), the delivered one first.
func TestSteerIsReturnedWhenTheTurnIsCancelled(t *testing.T) {
	repo := openObserveRepo(t)
	const sid = "s-steer-cancel"
	first := Steer{ID: "s1", Text: "stop, wrong file", Source: steerSourceKeyboard}
	second := Steer{ID: "s2", Text: "and revert it", Source: steerSourceKeyboard}
	var runner *Runner
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider := &abortProv{}
	runner = compactRunner(t, repo, provider, workRegistry(func() {
		accepted, err := runner.Steer(sid, first)
		assert.NoErr(t, err)
		assert.Eq(t, true, accepted)
	}), nil)
	_, err := runner.NewSession(context.Background(), sid, "steer cancel")
	assert.Require(t, assert.NoErr(t, err))
	rec := &eventRecorder{on: func(ev Event) {
		if ev.Kind != "steer_delivered" || ev.SteerID != first.ID {
			return
		}
		// The first steer is in the request that is about to go out; a second one
		// arrives after it, then the turn is aborted.
		accepted, err := runner.Steer(sid, second)
		assert.NoErr(t, err)
		assert.Eq(t, true, accepted)
		cancel()
	}}

	res, err := runner.Run(ctx, RunRequest{SessionID: sid, Prompt: "run the job", Emit: rec.emit})
	// An aborted turn comes back as a cancellation error (the runtime saves the
	// failed state on the cancelled context) with the cancelled outcome.
	assert.Require(t, assert.True(t, errors.Is(err, context.Canceled)))
	assert.Eq(t, RunCanceled, res.State)
	assert.Eq(t, []Steer{first, second}, res.ReturnedSteers)

	t.Run("the aborted request carried the first steer only", func(t *testing.T) {
		assert.Require(t, assert.Len(t, provider.reqs, 2))
		assert.Eq(t, 1, occurrences(provider.reqs[1].Messages, first.Text))
		assert.Eq(t, -1, indexOf(provider.reqs[1].Messages, second.Text))
	})

	t.Run("both are returned as cancelled, delivered one first", func(t *testing.T) {
		delivered := steerEventsOf(t, repo, sid, "delivered")
		assert.Require(t, assert.Len(t, delivered, 1))
		assert.Eq(t, "s1", delivered[0].Attrs["steer_id"])
		returned := steerEventsOf(t, repo, sid, "returned")
		assert.Require(t, assert.Len(t, returned, 2))
		assert.Eq(t, []string{"s1", "s2"}, []string{returned[0].Attrs["steer_id"].(string), returned[1].Attrs["steer_id"].(string)})
		for _, e := range returned {
			assert.Eq(t, "cancelled", e.Attrs["reason"])
			assert.Eq(t, "input_line", e.Attrs["disposition"])
		}
		assert.Len(t, rec.kinds("steer_returned"), 2)
	})
}

// pushProv pushes a steer while its final reply is in flight (the turn's last
// BuildMessages already happened) and keeps its requests.
type pushProv struct {
	mu    sync.Mutex
	reqs  []*agentcore.GenerateRequest
	push  func()
	calls int
}

func (p *pushProv) Name() string { return "push" }

func (p *pushProv) Generate(_ context.Context, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.calls++
	n := p.calls
	p.mu.Unlock()
	if n == 1 {
		return workCall("c1")
	}
	if p.push != nil {
		p.push()
	}
	return finalText("done")
}

// A completed turn returns only the steers no request took: the delivered one
// entered the turn's history and stays there (design D4). And once the turn's
// outcome is decided, the inbox no longer accepts anything.
func TestSteerUntakenIsReturnedWhenTheTurnCompletes(t *testing.T) {
	repo := openObserveRepo(t)
	const sid = "s-steer-complete"
	delivered := Steer{ID: "s1", Text: "only change the README", Source: steerSourceKeyboard}
	late := Steer{ID: "s2", Text: "one more thing", Source: steerSourceKeyboard}
	var runner *Runner
	refusedLate := true
	provider := &pushProv{push: func() {
		accepted, err := runner.Steer(sid, late)
		assert.NoErr(t, err)
		assert.Eq(t, true, accepted)
	}}
	runner = compactRunner(t, repo, provider, workRegistry(func() {
		accepted, err := runner.Steer(sid, delivered)
		assert.NoErr(t, err)
		assert.Eq(t, true, accepted)
	}), nil)
	_, err := runner.NewSession(context.Background(), sid, "steer complete")
	assert.Require(t, assert.NoErr(t, err))
	rec := &eventRecorder{on: func(ev Event) {
		// The turn's outcome is decided: a steer arriving before the caller is
		// told is not accepted any more.
		if ev.Kind != "run_finished" {
			return
		}
		accepted, err := runner.Steer(sid, Steer{ID: "s3", Text: "too late"})
		assert.NoErr(t, err)
		refusedLate = !accepted
	}}

	res, err := runner.Run(context.Background(), RunRequest{SessionID: sid, Prompt: "run the job", Emit: rec.emit})
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, "done", res.Output)
	assert.Eq(t, []Steer{late}, res.ReturnedSteers)
	assert.Eq(t, true, refusedLate) // Steer said no while the turn was closing

	t.Run("the delivery of the tool-call step stays in the turn", func(t *testing.T) {
		trace := steerEventsOf(t, repo, sid, "delivered")
		assert.Require(t, assert.Len(t, trace, 1))
		assert.Eq(t, "s1", trace[0].Attrs["steer_id"])
		assert.Eq(t, 1, occurrences(provider.reqs[1].Messages, delivered.Text))
	})
	t.Run("only the late steer is returned, as the next turn", func(t *testing.T) {
		returned := steerEventsOf(t, repo, sid, "returned")
		assert.Require(t, assert.Len(t, returned, 1))
		assert.Eq(t, "s2", returned[0].Attrs["steer_id"])
		assert.Eq(t, "completed_untaken", returned[0].Attrs["reason"])
		assert.Eq(t, "next_turn", returned[0].Attrs["disposition"])
		emitted := rec.kinds("steer_returned")
		assert.Require(t, assert.Len(t, emitted, 1))
		assert.Eq(t, "s2", emitted[0].SteerID)
	})
}

// gateProv blocks inside its first model call until the test releases it, so a
// test can fill the inbox while a turn is running.
type gateProv struct {
	entered chan struct{}
	release chan struct{}
	calls   int
}

func (p *gateProv) Name() string { return "gate" }

func (p *gateProv) Generate(context.Context, *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	p.calls++
	if p.calls == 1 {
		close(p.entered)
		<-p.release
	}
	return finalText("done")
}

func TestSteerInboxIsFull(t *testing.T) {
	repo := openObserveRepo(t)
	const sid = "s-steer-full"
	prov := &gateProv{entered: make(chan struct{}), release: make(chan struct{})}
	runner := compactRunner(t, repo, prov, nil, nil)
	_, err := runner.NewSession(context.Background(), sid, "steer full")
	assert.Require(t, assert.NoErr(t, err))

	done := make(chan RunResult, 1)
	go func() {
		res, err := runner.Run(context.Background(), RunRequest{SessionID: sid, Prompt: "run the job"})
		assert.NoErr(t, err)
		done <- res
	}()
	<-prov.entered // the turn runs and no request will take a steer now

	for i := 0; i < steerCapacity; i++ {
		accepted, err := runner.Steer(sid, Steer{ID: fmt.Sprintf("s%d", i), Text: fmt.Sprintf("steer %d", i)})
		assert.NoErr(t, err)
		assert.Eq(t, true, accepted)
	}
	accepted, err := runner.Steer(sid, Steer{ID: "overflow", Text: "one too many"})
	assert.Eq(t, false, accepted)
	assert.ErrIs(t, err, ErrSteerQueueFull)

	close(prov.release)
	res := <-done
	assert.Eq(t, "done", res.Output)
	assert.Len(t, res.ReturnedSteers, steerCapacity)
	assert.Eq(t, "s0", res.ReturnedSteers[0].ID)
}

// A Steer that arrives before a turn of the session runs (or while its Run is
// still getting there) is not accepted: it is the caller's next turn instead.
func TestSteerBeforeTheTurnRuns(t *testing.T) {
	repo := openObserveRepo(t)
	const sid = "s-steer-idle"
	runner := compactRunner(t, repo, &scriptProv{name: "main", normal: func(_ int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		return finalText("done")
	}}, nil, nil)
	_, err := runner.NewSession(context.Background(), sid, "steer idle")
	assert.Require(t, assert.NoErr(t, err))

	t.Run("no turn of the session is running", func(t *testing.T) {
		accepted, err := runner.Steer(sid, Steer{ID: "s1", Text: "hello"})
		assert.NoErr(t, err)
		assert.Eq(t, false, accepted)
	})

	t.Run("a Run still waiting for the session gate", func(t *testing.T) {
		// Holding the gate parks the Run long before it opens the inbox; a steer
		// arriving in that window is refused at any moment of the wait.
		release, err := runner.gate(sid).Acquire(context.Background())
		assert.Require(t, assert.NoErr(t, err))
		done := make(chan RunResult, 1)
		go func() {
			res, err := runner.Run(context.Background(), RunRequest{SessionID: sid, Prompt: "hi"})
			assert.NoErr(t, err)
			done <- res
		}()
		time.Sleep(20 * time.Millisecond) // let the Run reach the gate
		accepted, err := runner.Steer(sid, Steer{ID: "s2", Text: "hello"})
		assert.NoErr(t, err)
		assert.Eq(t, false, accepted)
		release()
		res := <-done
		assert.Eq(t, RunCompleted, res.State)
		assert.Empty(t, res.ReturnedSteers)
	})

	t.Run("a Run that ends before the runtime", func(t *testing.T) {
		// An archived session is refused before the inbox opens.
		model, err := runner.LoadSession(context.Background(), sid)
		assert.Require(t, assert.NoErr(t, err))
		model.Status = store.SessionArchived
		assert.Require(t, assert.NoErr(t, repo.UpdateSessionFields(context.Background(), model)))
		_, err = runner.Run(context.Background(), RunRequest{SessionID: sid, Prompt: "hi"})
		assert.ErrMsgContains(t, err, "not ready for a new turn")
		accepted, err := runner.Steer(sid, Steer{ID: "s3", Text: "hello"})
		assert.NoErr(t, err)
		assert.Eq(t, false, accepted)
		assert.Len(t, runner.steers, 0) // no inbox was left open
	})
}
