package personal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
)

// waitFor polls cond (the gate has no observable "waiting" hook).
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func gateWaiters(r *Runner, sid string) int {
	r.gateMu.Lock()
	defer r.gateMu.Unlock()
	if st := r.gates[sid]; st != nil {
		return st.waiters
	}
	return 0
}

func TestSessionGate(t *testing.T) {
	r := &Runner{}
	ctx := context.Background()
	t.Run("try acquire fails while held", func(t *testing.T) {
		release, err := r.gate("s1").Acquire(ctx)
		assert.NoErr(t, err)
		_, ok := r.gate("s1").TryAcquire()
		assert.False(t, ok)
		other, ok := r.gate("s2").TryAcquire() // another session is independent
		assert.True(t, ok)
		other()
		release()
		release() // idempotent
		again, ok := r.gate("s1").TryAcquire()
		assert.True(t, ok)
		again()
		assert.Eq(t, 0, len(r.gates)) // idle states are dropped
	})
	t.Run("try acquire fails while a preemptible holder holds", func(t *testing.T) {
		release, preempted, err := r.gate("s1").AcquirePreemptible(ctx)
		assert.NoErr(t, err)
		_, ok := r.gate("s1").TryAcquire()
		assert.False(t, ok)
		select {
		case <-preempted:
			t.Fatal("TryAcquire must not preempt")
		default:
		}
		release()
	})
	t.Run("acquire preempts and waits", func(t *testing.T) {
		release, preempted, err := r.gate("s1").AcquirePreemptible(ctx)
		assert.NoErr(t, err)
		var got atomic.Bool
		done := make(chan struct{})
		go func() {
			defer close(done)
			rel, err := r.gate("s1").Acquire(ctx)
			assert.NoErr(t, err)
			got.Store(true)
			rel()
		}()
		<-preempted
		time.Sleep(20 * time.Millisecond)
		assert.False(t, got.Load()) // still waiting for the release
		release()
		<-done
		assert.True(t, got.Load())
	})
	t.Run("preemptible taken while someone waits yields at once", func(t *testing.T) {
		// The race AcquirePreemptible can win against a waking Acquire.
		st := &gateState{waiters: 1}
		r.gateMu.Lock()
		rel := r.gate("sx").takeLocked(st, true)
		preempt := st.preempt
		r.gateMu.Unlock()
		select {
		case <-preempt:
		default:
			t.Fatal("not preempted although someone waits")
		}
		rel()
	})
	t.Run("try acquire preemptible never waits", func(t *testing.T) {
		hold, err := r.gate("s1").Acquire(ctx)
		assert.NoErr(t, err)
		_, _, ok := r.gate("s1").TryAcquirePreemptible()
		assert.False(t, ok) // a regular holder: give up at once
		assert.Eq(t, 0, gateWaiters(r, "s1"))
		hold()

		release, preempted, ok := r.gate("s1").TryAcquirePreemptible()
		assert.Require(t, assert.True(t, ok))
		_, _, ok = r.gate("s1").TryAcquirePreemptible()
		assert.False(t, ok) // a preemptible holder is not preempted by another try
		select {
		case <-preempted:
			t.Fatal("TryAcquirePreemptible must not preempt")
		default:
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			rel, err := r.gate("s1").Acquire(ctx)
			assert.NoErr(t, err)
			rel()
		}()
		<-preempted // an Acquire asks the try-taken holder to yield
		release()
		<-done
		assert.Eq(t, 0, len(r.gates))
	})
	t.Run("acquire gives up with ctx", func(t *testing.T) {
		hold, err := r.gate("s1").Acquire(ctx)
		assert.NoErr(t, err)
		cctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
		defer cancel()
		_, err = r.gate("s1").Acquire(cctx)
		assert.True(t, errors.Is(err, context.DeadlineExceeded))
		_, _, err = r.gate("s1").AcquirePreemptible(cctx)
		assert.True(t, errors.Is(err, context.DeadlineExceeded))
		assert.Eq(t, 0, gateWaiters(r, "s1"))
		hold()
	})
}

// Two Runs of one session in one process do not overlap: the second waits
// for the first instead of failing on the running status.
func TestConcurrentRunsAreSerialized(t *testing.T) {
	repo := openObserveRepo(t)
	entered, proceed := make(chan struct{}, 4), make(chan struct{})
	var inFlight, maxInFlight atomic.Int32
	prov := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		cur := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			old := maxInFlight.Load()
			if cur <= old || maxInFlight.CompareAndSwap(old, cur) {
				break
			}
		}
		if n == 2 { // the first Run of the session
			entered <- struct{}{}
			<-proceed
		}
		return finalText(fmt.Sprintf("reply-%d", n))
	}}
	runner := compactRunner(t, repo, prov, nil, nil)
	ctx := context.Background()
	first, err := runner.Run(ctx, RunRequest{Prompt: "create"})
	assert.NoErr(t, err)
	sid := first.SessionID

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, errs[0] = runner.Run(ctx, RunRequest{SessionID: sid, Prompt: "one"})
	}()
	<-entered
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, errs[1] = runner.Run(ctx, RunRequest{SessionID: sid, Prompt: "two"})
	}()
	waitFor(t, "second run at the gate", func() bool { return gateWaiters(runner, sid) == 1 })
	close(proceed)
	wg.Wait()
	assert.NoErr(t, errs[0])
	assert.NoErr(t, errs[1])
	assert.Eq(t, int32(1), maxInFlight.Load())
	assert.Eq(t, 3, len(prov.reqs))
}

// blockingSumProv blocks the first summary call until its ctx ends (an idle
// consolidation in flight); later summaries answer at once.
type blockingSumProv struct {
	*scriptProv
	entered chan struct{}
	calls   atomic.Int32
}

func (p *blockingSumProv) Generate(ctx context.Context, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	if req.System == compactSystemPrompt && p.calls.Add(1) == 1 {
		close(p.entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return p.scriptProv.Generate(ctx, req)
}

// A Run or Compact arriving during an idle consolidation preempts it: the
// consolidation stops before its commit point (cancelled event, no record,
// meta untouched) and the newcomer proceeds; only one new segment record.
func TestIdleConsolidationIsPreempted(t *testing.T) {
	for _, by := range []string{"compact", "run"} {
		t.Run(by, func(t *testing.T) {
			repo := openObserveRepo(t)
			sp := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
				return finalText(fmt.Sprintf("reply-%d", n))
			}}
			prov := &blockingSumProv{scriptProv: sp, entered: make(chan struct{})}
			runner := compactRunner(t, repo, prov, nil, func(c *Config) { c.ContextWindow, c.CompactReserveTokens = 100000, 1000 })
			ctx := context.Background()
			res, err := runner.Run(ctx, RunRequest{Prompt: "hello " + strings.Repeat("h", 400)})
			assert.NoErr(t, err)
			sid := res.SessionID
			before, err := repo.GetSession(ctx, sid)
			assert.NoErr(t, err)

			idleErr := make(chan error, 1)
			go func() {
				_, err := runner.ConsolidateIdle(ctx, sid)
				idleErr <- err
			}()
			<-prov.entered
			// Bounded, so a missing preemption fails instead of hanging.
			nctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			if by == "compact" {
				out, err := runner.Compact(nctx, sid)
				assert.Require(t, assert.NoErr(t, err))
				assert.Eq(t, 2, out.Segment)
			} else {
				_, err := runner.Run(nctx, RunRequest{SessionID: sid, Prompt: "typed while idle"})
				assert.Require(t, assert.NoErr(t, err))
			}
			select {
			case err = <-idleErr:
			case <-time.After(10 * time.Second):
				t.Fatal("idle consolidation did not return")
			}
			assert.True(t, errors.Is(err, ErrPreempted))
			assert.True(t, errors.Is(err, context.Canceled))

			recs := compactionEntries(t, repo, sid)
			var idle int
			for _, rec := range recs {
				if rec.Reason == segmentReasonIdle {
					idle++
				}
			}
			assert.Eq(t, 0, idle)
			if by == "compact" {
				assert.Eq(t, 1, len(recs))
				assert.Eq(t, segmentReasonManual, recs[0].Reason)
			} else {
				assert.Eq(t, 0, len(recs))
				after, err := repo.GetSession(ctx, sid)
				assert.NoErr(t, err)
				assert.Eq(t, metaString(before.Meta, "personal_rules_hash"), metaString(after.Meta, "personal_rules_hash"))
			}
			var cancelled int
			for _, e := range eventsOf(t, repo, sid, store.EventCompaction) {
				if e.Status == eventCancelled {
					cancelled++
					assert.Eq(t, segmentReasonIdle, e.Attrs["reason"])
					assert.Eq(t, "", e.Error)
				}
				assert.True(t, e.Status != eventError)
			}
			assert.Eq(t, 1, cancelled)
		})
	}
}

// cancelOnOK cancels a ctx when the segment break's ok event is recorded,
// i.e. right after the record was written.
type cancelOnOK struct {
	eventSink
	cancel context.CancelFunc
}

func (s cancelOnOK) AppendEvent(ctx context.Context, e store.Event) (store.Event, error) {
	if e.Kind == store.EventCompaction && e.Status == eventOK {
		s.cancel()
	}
	return s.eventSink.AppendEvent(ctx, e)
}

// The segment record is the single commit point: cancelled before it, nothing
// is written; cancelled after it, the break completes.
func TestSegmentCommitPoint(t *testing.T) {
	setup := func(t *testing.T, sum func(ctx context.Context) string) (*Runner, *store.Store, string) {
		repo := openObserveRepo(t)
		sp := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
			return finalText(fmt.Sprintf("reply-%d", n))
		}}
		runner := compactRunner(t, repo, ctxSumProv{sp, sum}, nil, func(c *Config) { c.ContextWindow, c.CompactReserveTokens = 100000, 1000 })
		res, err := runner.Run(context.Background(), RunRequest{Prompt: "hello " + strings.Repeat("h", 400)})
		assert.NoErr(t, err)
		return runner, repo, res.SessionID
	}
	t.Run("cancelled before the record: nothing written", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		// The summary itself succeeds; the cancellation lands right after it.
		runner, repo, sid := setup(t, func(context.Context) string { cancel(); return "PKG-BODY" })
		before, err := repo.GetSession(context.Background(), sid)
		assert.NoErr(t, err)
		_, err = runner.ConsolidateIdle(ctx, sid)
		assert.True(t, errors.Is(err, context.Canceled))
		assert.False(t, errors.Is(err, ErrPreempted))
		assert.Eq(t, 0, len(compactionEntries(t, repo, sid)))
		after, err := repo.GetSession(context.Background(), sid)
		assert.NoErr(t, err)
		assert.Eq(t, before.Meta, after.Meta)
		evs := eventsOf(t, repo, sid, store.EventCompaction)
		assert.Require(t, assert.Eq(t, 1, len(evs)))
		assert.Eq(t, eventCancelled, evs[0].Status)
	})
	t.Run("cancelled after the record: the break stands", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		runner, repo, sid := setup(t, func(context.Context) string { return "PKG-BODY" })
		runner.rec.sink = cancelOnOK{eventSink: runner.rec.sink, cancel: cancel}
		out, err := runner.ConsolidateIdle(ctx, sid)
		assert.NoErr(t, err)
		assert.Eq(t, 2, out.Segment)
		assert.True(t, ctx.Err() != nil)
		recs := compactionEntries(t, repo, sid)
		assert.Require(t, assert.Eq(t, 1, len(recs)))
		assert.Eq(t, segmentReasonIdle, recs[0].Reason)
		// The meta cache was still refreshed and the next turn uses the package.
		view, err := runner.loadHistory(context.Background(), sid)
		assert.NoErr(t, err)
		assert.StrContains(t, view.summary, "PKG-BODY")
		evs := eventsOf(t, repo, sid, store.EventCompaction)
		assert.Require(t, assert.Eq(t, 1, len(evs)))
		assert.Eq(t, eventOK, evs[0].Status)
		assert.Eq(t, "idle", evs[0].Attrs["trigger"])
	})
}

// ctxSumProv answers summary calls with sum(ctx).
type ctxSumProv struct {
	*scriptProv
	sum func(ctx context.Context) string
}

func (p ctxSumProv) Generate(ctx context.Context, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	if req.System == compactSystemPrompt {
		return finalText(p.sum(ctx))
	}
	return p.scriptProv.Generate(ctx, req)
}

// The idle consolidation never waits for the gate: while a Run or Compact
// holds it, ConsolidateIdle returns ErrBusy at once and writes nothing.
func TestConsolidateIdleBusy(t *testing.T) {
	repo := openObserveRepo(t)
	sp := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		return finalText(fmt.Sprintf("reply-%d", n))
	}}
	runner := compactRunner(t, repo, ctxSumProv{sp, func(context.Context) string { return "PKG-BODY" }}, nil,
		func(c *Config) { c.ContextWindow, c.CompactReserveTokens = 100000, 1000 })
	ctx := context.Background()
	res, err := runner.Run(ctx, RunRequest{Prompt: "hello " + strings.Repeat("h", 400)})
	assert.NoErr(t, err)
	sid := res.SessionID

	t.Run("busy while held", func(t *testing.T) {
		hold, err := runner.gate(sid).Acquire(ctx)
		assert.Require(t, assert.NoErr(t, err))
		done := make(chan error, 1)
		go func() {
			_, err := runner.ConsolidateIdle(ctx, sid)
			done <- err
		}()
		select {
		case err = <-done:
		case <-time.After(5 * time.Second):
			hold()
			t.Fatal("ConsolidateIdle waited for the gate")
		}
		hold()
		assert.True(t, errors.Is(err, ErrBusy))
		assert.Eq(t, 0, len(compactionEntries(t, repo, sid)))
		assert.Eq(t, 0, len(eventsOf(t, repo, sid, store.EventCompaction)))
	})
	t.Run("free gate consolidates", func(t *testing.T) {
		out, err := runner.ConsolidateIdle(ctx, sid)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, 2, out.Segment)
		recs := compactionEntries(t, repo, sid)
		assert.Require(t, assert.Eq(t, 1, len(recs)))
		assert.Eq(t, segmentReasonIdle, recs[0].Reason)
	})
}

func TestContextThreshold(t *testing.T) {
	repo := openObserveRepo(t)
	sp := &scriptProv{name: "main"}
	t.Run("from the window", func(t *testing.T) {
		r := compactRunner(t, repo, sp, nil, func(c *Config) { c.ContextWindow, c.CompactReserveTokens = 1000, 0 })
		assert.Eq(t, 800, r.ContextThreshold()) // reserve = window*20%
	})
	t.Run("default window", func(t *testing.T) {
		r := compactRunner(t, repo, sp, nil, func(c *Config) { c.ContextWindow, c.CompactReserveTokens = 0, 0 })
		assert.Eq(t, defaultContextWindow-defaultReserveTokens, r.ContextThreshold())
	})
}

func TestRecordHostEvent(t *testing.T) {
	repo := openObserveRepo(t)
	runner := compactRunner(t, repo, &scriptProv{name: "main"}, nil, nil)
	ctx := context.Background()
	sess, err := runner.NewSession(ctx, "", "t")
	assert.NoErr(t, err)
	runner.RecordEvent(ctx, sess.ID, HostEvent{Kind: store.EventSchedule, Name: "idle-consolidate", Status: "fired",
		Duration: 1500 * time.Millisecond, Error: "x", Attrs: map[string]any{"event": "fired"}})
	runner.RecordEvent(ctx, "", HostEvent{Kind: store.EventSchedule, Name: "dropped"}) // no session: skipped
	evs := eventsOf(t, repo, sess.ID, store.EventSchedule)
	assert.Require(t, assert.Eq(t, 1, len(evs)))
	assert.Eq(t, "idle-consolidate", evs[0].Name)
	assert.Eq(t, "fired", evs[0].Status)
	assert.Eq(t, int64(1500), evs[0].DurationMs)
	assert.Eq(t, "x", evs[0].Error)
	assert.Eq(t, "fired", evs[0].Attrs["event"])
	assert.Eq(t, "", evs[0].TurnID)
}
