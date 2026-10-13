package schedule_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/inhere/suga/pkg/agentcore/schedule"
)

// Black-box conformance tests on the public API with the real clock and short
// durations (kept from the three-way comparison that selected this implementation).

const d = 60 * time.Millisecond

func count(n *atomic.Int32) schedule.Func {
	return func(context.Context) error { n.Add(1); return nil }
}

func TestConfAfterOnce(t *testing.T) {
	s := schedule.New()
	defer s.Close()
	var n atomic.Int32
	s.After("a", d, count(&n))
	time.Sleep(4 * d)
	if n.Load() != 1 {
		t.Fatalf("After fired %d times, want 1", n.Load())
	}
}

func TestConfIdleTouchAndOnce(t *testing.T) {
	s := schedule.New()
	defer s.Close()
	var n atomic.Int32
	s.Idle("i", d, count(&n))
	for i := 0; i < 8; i++ { // keep busy for ~8*d/3
		time.Sleep(d / 3)
		s.Touch()
	}
	if n.Load() != 0 {
		t.Fatalf("Idle fired while touched: %d", n.Load())
	}
	time.Sleep(4 * d)
	if n.Load() != 1 {
		t.Fatalf("Idle fired %d times after going idle, want 1 (no refire without Touch)", n.Load())
	}
	s.Touch()
	time.Sleep(3 * d)
	if n.Load() != 2 {
		t.Fatalf("Idle should fire again after a new Touch: %d", n.Load())
	}
}

func TestConfPauseResume(t *testing.T) {
	s := schedule.New()
	defer s.Close()
	var n atomic.Int32
	h := s.Idle("p", d, count(&n))
	h.Pause()
	time.Sleep(3 * d)
	if n.Load() != 0 {
		t.Fatalf("fired while paused: %d", n.Load())
	}
	h.Resume()
	time.Sleep(d / 2)
	if n.Load() != 0 {
		t.Fatalf("Resume must restart the timer from now")
	}
	time.Sleep(3 * d)
	if n.Load() != 1 {
		t.Fatalf("after Resume fired %d, want 1", n.Load())
	}
}

func TestConfReplaceSameName(t *testing.T) {
	s := schedule.New()
	defer s.Close()
	var old, cur atomic.Int32
	s.After("x", d, count(&old))
	s.After("x", 2*d, count(&cur))
	time.Sleep(4 * d)
	if old.Load() != 0 || cur.Load() != 1 {
		t.Fatalf("replace: old=%d cur=%d, want 0/1", old.Load(), cur.Load())
	}
}

func TestConfNoConcurrentRun(t *testing.T) {
	s := schedule.New()
	defer s.Close()
	var running, maxRunning, runs atomic.Int32
	s.Every("e", d/3, func(ctx context.Context) error {
		r := running.Add(1)
		for {
			m := maxRunning.Load()
			if r <= m || maxRunning.CompareAndSwap(m, r) {
				break
			}
		}
		runs.Add(1)
		time.Sleep(d)
		running.Add(-1)
		return nil
	})
	time.Sleep(5 * d)
	if maxRunning.Load() != 1 {
		t.Fatalf("task ran concurrently: max=%d", maxRunning.Load())
	}
	if runs.Load() < 2 {
		t.Fatalf("Every should keep firing: runs=%d", runs.Load())
	}
}

func TestConfCloseCancelsAndWaits(t *testing.T) {
	s := schedule.New()
	var done atomic.Bool
	started := make(chan struct{})
	s.After("c", time.Millisecond, func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		time.Sleep(d / 2)
		done.Store(true)
		return ctx.Err()
	})
	<-started
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !done.Load() {
		t.Fatal("Close returned before the running task finished")
	}
	h := s.After("late", time.Millisecond, func(context.Context) error { t.Error("ran after Close"); return nil })
	h.Cancel()
	h.Reset()
	h.Pause()
	h.Resume()
	time.Sleep(d)
}

func TestConfCancelRunning(t *testing.T) {
	s := schedule.New()
	defer s.Close()
	got := make(chan error, 1)
	started := make(chan struct{})
	h := s.After("r", time.Millisecond, func(ctx context.Context) error {
		close(started)
		select {
		case <-ctx.Done():
			got <- ctx.Err()
		case <-time.After(10 * d):
			got <- errors.New("not cancelled")
		}
		return nil
	})
	<-started
	h.Cancel()
	if err := <-got; !errors.Is(err, context.Canceled) {
		t.Fatalf("running task not cancelled: %v", err)
	}
}

func TestConfEventsAndObserverReentry(t *testing.T) {
	var mu sync.Mutex
	kinds := map[string]bool{}
	s := schedule.New(schedule.WithObserver(func(e schedule.Event) {
		mu.Lock()
		kinds[fmt.Sprint(e.Kind)] = true
		mu.Unlock()
	}))
	defer s.Close()
	h := s.Idle("ev", d, func(context.Context) error { return errors.New("boom") })
	h.Pause()
	h.Resume()
	time.Sleep(3 * d)
	h.Cancel()
	time.Sleep(d / 2)
	mu.Lock()
	defer mu.Unlock()
	for _, k := range []string{"scheduled", "fired", "failed", "paused", "resumed", "cancelled"} {
		if !kinds[k] {
			t.Errorf("missing event kind %q (got %v)", k, kinds)
		}
	}
}

// Reentry: an observer that calls back into the scheduler (e.g. Touch) must
// not deadlock. Reported separately, since the spec did not require it.
func TestConfObserverReentryNoDeadlock(t *testing.T) {
	var s schedule.Scheduler
	ready := make(chan struct{})
	s = schedule.New(schedule.WithObserver(func(e schedule.Event) {
		<-ready
		s.Touch()
	}))
	close(ready)
	finished := make(chan struct{})
	go func() {
		s.After("re", time.Millisecond, func(context.Context) error { return nil })
		time.Sleep(3 * d)
		_ = s.Close()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(20 * d):
		t.Fatal("deadlock: observer calling Touch blocked the scheduler")
	}
}
