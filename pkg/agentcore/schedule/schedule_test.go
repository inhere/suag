package schedule

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
)

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	clock   *fakeClock
	at      time.Time
	fn      func()
	stopped bool
}

func newFakeClock() *fakeClock      { return &fakeClock{now: time.Unix(100, 0)} }
func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) AfterFunc(d time.Duration, fn func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{clock: c, at: c.now.Add(d), fn: fn}
	c.timers = append(c.timers, t)
	return t
}
func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	wasActive := !t.stopped
	t.stopped = true
	return wasActive
}

// Advance invokes due callbacks outside the clock lock, just like real timers.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	target := c.now.Add(d)
	c.mu.Unlock()
	for {
		c.mu.Lock()
		var next *fakeTimer
		for _, timer := range c.timers {
			if !timer.stopped && !timer.at.After(target) && (next == nil || timer.at.Before(next.at)) {
				next = timer
			}
		}
		if next == nil {
			c.now = target
			c.mu.Unlock()
			return
		}
		next.stopped = true
		c.now = next.at
		c.mu.Unlock()
		next.fn()
	}
}

type eventLog struct {
	mu     sync.Mutex
	events []Event
	ch     chan Event
}

func newEventLog() *eventLog { return &eventLog{ch: make(chan Event, 100)} }
func (l *eventLog) observe(e Event) {
	l.mu.Lock()
	l.events = append(l.events, e)
	l.mu.Unlock()
	l.ch <- e
}
func (l *eventLog) count(kind EventKind) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, e := range l.events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}
func (l *eventLog) await(t *testing.T, kind EventKind) Event {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-l.ch:
			if e.Kind == kind {
				return e
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s event", kind)
			return Event{}
		}
	}
}

func TestAfter(t *testing.T) {
	clock, log := newFakeClock(), newEventLog()
	s := New(WithClock(clock), WithObserver(log.observe))
	defer s.Close()
	called := make(chan struct{}, 2)
	s.After("once", 10*time.Second, func(context.Context) error { called <- struct{}{}; return nil })
	clock.Advance(9 * time.Second)
	assert.Eq(t, 0, log.count(Fired))
	clock.Advance(time.Second)
	log.await(t, Done)
	clock.Advance(30 * time.Second)
	assert.Eq(t, 1, len(called))
	assert.Eq(t, 1, log.count(Fired))
}

func TestAfterCannotRearmWhileRunning(t *testing.T) {
	clock, log := newFakeClock(), newEventLog()
	s := New(WithClock(clock), WithObserver(log.observe))
	started, release := make(chan struct{}), make(chan struct{})
	h := s.After("once", time.Second, func(context.Context) error {
		close(started)
		<-release
		return nil
	})
	clock.Advance(time.Second)
	<-started
	h.Reset()
	h.Pause()
	h.Resume()
	clock.Advance(10 * time.Second)
	assert.Eq(t, 1, log.count(Fired))
	close(release)
	log.await(t, Done)
	assert.Eq(t, error(nil), s.Close())
}

func TestEvery(t *testing.T) {
	clock, log := newFakeClock(), newEventLog()
	s := New(WithClock(clock), WithObserver(log.observe))
	defer s.Close()
	count := 0
	var mu sync.Mutex
	s.Every("tick", 5*time.Second, func(context.Context) error { mu.Lock(); count++; mu.Unlock(); return nil })
	for i := 0; i < 3; i++ {
		clock.Advance(5 * time.Second)
		log.await(t, Done)
	}
	mu.Lock()
	got := count
	mu.Unlock()
	assert.Eq(t, 3, got)
}

func TestIdle(t *testing.T) {
	clock, log := newFakeClock(), newEventLog()
	s := New(WithClock(clock), WithObserver(log.observe))
	defer s.Close()
	s.Idle("idle", 10*time.Second, func(context.Context) error { return nil })
	clock.Advance(9 * time.Second)
	s.Touch()
	clock.Advance(9 * time.Second)
	assert.Eq(t, 0, log.count(Fired))
	clock.Advance(time.Second)
	log.await(t, Done)
	clock.Advance(50 * time.Second)
	assert.Eq(t, 1, log.count(Fired))
	s.Touch()
	clock.Advance(10 * time.Second)
	log.await(t, Done)
	assert.Eq(t, 2, log.count(Fired))
}

func TestPauseResume(t *testing.T) {
	for _, mode := range []string{"after", "every", "idle"} {
		t.Run(mode, func(t *testing.T) {
			clock, log := newFakeClock(), newEventLog()
			s := New(WithClock(clock), WithObserver(log.observe))
			defer s.Close()
			fn := func(context.Context) error { return nil }
			var h Handle
			switch mode {
			case "after":
				h = s.After(mode, 10*time.Second, fn)
			case "every":
				h = s.Every(mode, 10*time.Second, fn)
			case "idle":
				h = s.Idle(mode, 10*time.Second, fn)
			}
			clock.Advance(7 * time.Second)
			h.Pause()
			clock.Advance(20 * time.Second)
			assert.Eq(t, 0, log.count(Fired))
			h.Resume()
			clock.Advance(9 * time.Second)
			assert.Eq(t, 0, log.count(Fired))
			clock.Advance(time.Second)
			log.await(t, Done)
			assert.Eq(t, 1, log.count(Paused))
			assert.Eq(t, 1, log.count(Resumed))
		})
	}
}

func TestCancelReset(t *testing.T) {
	t.Run("reset from now", func(t *testing.T) {
		clock, log := newFakeClock(), newEventLog()
		s := New(WithClock(clock), WithObserver(log.observe))
		defer s.Close()
		h := s.After("reset", 10*time.Second, func(context.Context) error { return nil })
		clock.Advance(9 * time.Second)
		h.Reset()
		clock.Advance(9 * time.Second)
		assert.Eq(t, 0, log.count(Fired))
		clock.Advance(time.Second)
		log.await(t, Done)
	})
	t.Run("cancel", func(t *testing.T) {
		clock, log := newFakeClock(), newEventLog()
		s := New(WithClock(clock), WithObserver(log.observe))
		defer s.Close()
		h := s.Every("cancel", time.Second, func(context.Context) error { return nil })
		h.Cancel()
		h.Cancel()
		h.Reset()
		h.Resume()
		clock.Advance(10 * time.Second)
		assert.Eq(t, 0, log.count(Fired))
		assert.Eq(t, 1, log.count(Cancelled))
	})
	t.Run("cancel running function", func(t *testing.T) {
		clock, log := newFakeClock(), newEventLog()
		s := New(WithClock(clock), WithObserver(log.observe))
		defer s.Close()
		cancelled := make(chan struct{})
		h := s.After("running", time.Second, func(ctx context.Context) error {
			<-ctx.Done()
			close(cancelled)
			return nil
		})
		clock.Advance(time.Second)
		h.Cancel()
		<-cancelled
		log.await(t, Done)
		assert.Eq(t, 1, log.count(Cancelled))
	})
}

func TestCloseWaitsForRunning(t *testing.T) {
	clock, log := newFakeClock(), newEventLog()
	s := New(WithClock(clock), WithObserver(log.observe))
	started, release := make(chan struct{}), make(chan struct{})
	s.After("running", time.Second, func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		<-release
		return nil
	})
	clock.Advance(time.Second)
	<-started
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	log.await(t, Cancelled)
	select {
	case <-closed:
		t.Fatal("Close returned before function ended")
	default:
	}
	close(release)
	assert.Eq(t, error(nil), <-closed)
	h := s.After("late", 0, func(context.Context) error { t.Fatal("late task ran"); return nil })
	h.Cancel()
	h.Reset()
	h.Pause()
	h.Resume()
	clock.Advance(time.Second)
	assert.Eq(t, 1, log.count(Fired))
	assert.Eq(t, error(nil), s.Close())
}

func TestSameNameReplacesRunningTask(t *testing.T) {
	clock, log := newFakeClock(), newEventLog()
	s := New(WithClock(clock), WithObserver(log.observe))
	defer s.Close()
	started, cancelled := make(chan struct{}), make(chan struct{})
	old := s.Every("same", time.Second, func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil
	})
	clock.Advance(time.Second)
	<-started
	s.After("same", 2*time.Second, func(context.Context) error { return nil })
	<-cancelled
	old.Reset()
	clock.Advance(2 * time.Second)
	log.await(t, Done)
	assert.Eq(t, 1, log.count(Cancelled))
	assert.Eq(t, 2, log.count(Fired))
}

func TestSkipConcurrentRun(t *testing.T) {
	clock, log := newFakeClock(), newEventLog()
	s := New(WithClock(clock), WithObserver(log.observe))
	defer s.Close()
	started, release := make(chan struct{}), make(chan struct{})
	s.Every("slow", time.Second, func(context.Context) error {
		close(started)
		<-release
		return nil
	})
	clock.Advance(time.Second)
	<-started
	clock.Advance(3 * time.Second)
	assert.Eq(t, 3, log.count(Skipped))
	assert.Eq(t, 1, log.count(Fired))
	close(release)
	log.await(t, Done)
}

func TestObserverEvents(t *testing.T) {
	clock, log := newFakeClock(), newEventLog()
	s := New(WithClock(clock), WithObserver(log.observe))
	defer s.Close()
	wantErr := errors.New("failed")
	h := s.After("events", time.Second, func(context.Context) error { return wantErr })
	h.Pause()
	h.Resume()
	clock.Advance(time.Second)
	e := log.await(t, Failed)
	assert.Eq(t, "events", e.Name)
	assert.Eq(t, wantErr, e.Err)
	assert.Eq(t, clock.Now(), e.At)
	assert.Eq(t, time.Duration(0), e.Duration)
	for _, kind := range []EventKind{Scheduled, Paused, Resumed, Fired, Failed} {
		assert.Require(t, assert.Eq(t, 1, log.count(kind)))
	}
}

func TestObserverCanCallScheduler(t *testing.T) {
	clock := newFakeClock()
	var s Scheduler
	s = New(WithClock(clock), WithObserver(func(e Event) {
		if e.Kind == Scheduled {
			s.Touch()
		}
	}))
	h := s.After("reentrant", time.Second, func(context.Context) error { return nil })
	h.Cancel()
	assert.Eq(t, error(nil), s.Close())
}

func TestObserverCanCloseOnFire(t *testing.T) {
	clock := newFakeClock()
	var s Scheduler
	var closeErr error
	s = New(WithClock(clock), WithObserver(func(e Event) {
		if e.Kind == Fired {
			closeErr = s.Close()
		}
	}))
	s.After("close", time.Second, func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})
	clock.Advance(time.Second)
	assert.Eq(t, error(nil), closeErr)
}

func TestEventDuration(t *testing.T) {
	clock, log := newFakeClock(), newEventLog()
	s := New(WithClock(clock), WithObserver(log.observe))
	started, release := make(chan struct{}), make(chan struct{})
	s.After("duration", time.Second, func(context.Context) error {
		close(started)
		<-release
		return nil
	})
	clock.Advance(time.Second)
	<-started
	clock.Advance(4 * time.Second)
	close(release)
	e := log.await(t, Done)
	assert.Eq(t, 4*time.Second, e.Duration)
	assert.Eq(t, error(nil), e.Err)
	assert.Eq(t, error(nil), s.Close())
}
