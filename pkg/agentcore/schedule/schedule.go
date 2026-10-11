// Package schedule provides in-process delayed, periodic, and idle triggers for
// agent hosts, such as chat-idle context cleanup and future self-triggering.
package schedule

import (
	"context"
	"sync"
	"time"
)

// Func is called with a context cancelled when its task is cancelled or the scheduler closes.
type Func func(ctx context.Context) error

type Handle interface {
	Cancel()
	Reset()
	Pause()
	Resume()
}

type Scheduler interface {
	After(name string, d time.Duration, fn Func) Handle
	Every(name string, d time.Duration, fn Func) Handle
	Idle(name string, d time.Duration, fn Func) Handle
	Touch()
	Close() error
}

type Timer interface{ Stop() bool }

type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Timer
}

type EventKind string

const (
	Scheduled EventKind = "scheduled"
	Fired     EventKind = "fired"
	Skipped   EventKind = "skipped"
	Cancelled EventKind = "cancelled"
	Paused    EventKind = "paused"
	Resumed   EventKind = "resumed"
	Done      EventKind = "done"
	Failed    EventKind = "failed"
)

type Event struct {
	Name     string
	Kind     EventKind
	Err      error
	Duration time.Duration
	At       time.Time
}

type Option func(*scheduler)

func WithClock(clock Clock) Option {
	return func(s *scheduler) {
		if clock != nil {
			s.clock = clock
		}
	}
}

func WithObserver(observer func(Event)) Option {
	return func(s *scheduler) { s.observer = observer }
}

func New(opts ...Option) Scheduler {
	s := &scheduler{clock: realClock{}, tasks: make(map[string]*task)}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

type realClock struct{}

func (realClock) Now() time.Time                            { return time.Now() }
func (realClock) AfterFunc(d time.Duration, f func()) Timer { return time.AfterFunc(d, f) }

type mode uint8

const (
	after mode = iota
	every
	idle
)

type scheduler struct {
	mu       sync.Mutex
	clock    Clock
	observer func(Event)
	tasks    map[string]*task
	closed   bool
	running  sync.WaitGroup
}

type task struct {
	s          *scheduler
	name       string
	d          time.Duration
	fn         Func
	mode       mode
	timer      Timer
	generation uint64
	active     bool
	paused     bool
	running    bool
	triggered  bool
	cancel     context.CancelFunc
}

type noopHandle struct{}

func (noopHandle) Cancel() {}
func (noopHandle) Reset()  {}
func (noopHandle) Pause()  {}
func (noopHandle) Resume() {}

func (s *scheduler) After(name string, d time.Duration, fn Func) Handle {
	return s.add(name, d, fn, after)
}
func (s *scheduler) Every(name string, d time.Duration, fn Func) Handle {
	return s.add(name, d, fn, every)
}
func (s *scheduler) Idle(name string, d time.Duration, fn Func) Handle {
	return s.add(name, d, fn, idle)
}

func (s *scheduler) add(name string, d time.Duration, fn Func, kind mode) Handle {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return noopHandle{}
	}
	var events []Event
	if old := s.tasks[name]; old != nil {
		events = append(events, old.cancelLocked())
	}
	t := &task{s: s, name: name, d: d, fn: fn, mode: kind, active: true}
	s.tasks[name] = t
	t.armLocked()
	events = append(events, t.eventLocked(Scheduled, nil, 0))
	s.mu.Unlock()
	s.emit(events...)
	return t
}

// armLocked invalidates old callbacks, including callbacks already queued by a clock.
func (t *task) armLocked() {
	if t.timer != nil {
		t.timer.Stop()
	}
	t.generation++
	gen := t.generation
	d := t.d
	if t.mode == every && d <= 0 {
		d = time.Nanosecond
	}
	t.timer = t.s.clock.AfterFunc(d, func() { t.fire(gen) })
}

func (t *task) eventLocked(kind EventKind, err error, duration time.Duration) Event {
	return Event{Name: t.name, Kind: kind, Err: err, Duration: duration, At: t.s.clock.Now()}
}

func (s *scheduler) emit(events ...Event) {
	if s.observer == nil {
		return
	}
	for _, event := range events {
		s.observer(event)
	}
}

func (t *task) fire(gen uint64) {
	s := t.s
	s.mu.Lock()
	if !t.active || t.paused || s.closed || gen != t.generation {
		s.mu.Unlock()
		return
	}
	t.timer = nil
	if t.mode == every {
		t.armLocked()
	}
	if t.running {
		event := t.eventLocked(Skipped, nil, 0)
		s.mu.Unlock()
		s.emit(event)
		return
	}
	// After remains registered during its run so Cancel and Close can stop it,
	// but it must never be rearmed. Idle waits for another Touch or Reset.
	if t.mode == after {
		t.triggered = true
	}
	t.running = true
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	s.running.Add(1)
	start := s.clock.Now()
	event := t.eventLocked(Fired, nil, 0)
	s.mu.Unlock()
	go t.run(ctx, start)
	s.emit(event)
}

func (t *task) run(ctx context.Context, start time.Time) {
	defer t.s.running.Done()
	err := t.fn(ctx)
	s := t.s
	s.mu.Lock()
	duration := s.clock.Now().Sub(start)
	if duration < 0 {
		duration = 0
	}
	t.running = false
	t.cancel = nil
	if t.mode == after && t.active {
		t.active = false
		if s.tasks[t.name] == t {
			delete(s.tasks, t.name)
		}
	}
	kind := Done
	if err != nil {
		kind = Failed
	}
	event := t.eventLocked(kind, err, duration)
	s.mu.Unlock()
	s.emit(event)
}

// cancelLocked removes the registration but leaves a running function to observe its context.
func (t *task) cancelLocked() Event {
	t.active = false
	t.generation++
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
	if t.cancel != nil {
		t.cancel()
	}
	if t.s.tasks[t.name] == t {
		delete(t.s.tasks, t.name)
	}
	return t.eventLocked(Cancelled, nil, 0)
}

func (t *task) Cancel() {
	s := t.s
	s.mu.Lock()
	if !t.active {
		s.mu.Unlock()
		return
	}
	event := t.cancelLocked()
	s.mu.Unlock()
	s.emit(event)
}

func (t *task) Reset() {
	s := t.s
	s.mu.Lock()
	if !t.active || s.closed || (t.mode == after && t.triggered) {
		s.mu.Unlock()
		return
	}
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
	t.generation++
	if !t.paused {
		t.armLocked()
	}
	event := t.eventLocked(Scheduled, nil, 0)
	s.mu.Unlock()
	s.emit(event)
}

func (t *task) Pause() {
	s := t.s
	s.mu.Lock()
	if !t.active || t.paused || s.closed || (t.mode == after && t.triggered) {
		s.mu.Unlock()
		return
	}
	t.paused = true
	t.generation++
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
	event := t.eventLocked(Paused, nil, 0)
	s.mu.Unlock()
	s.emit(event)
}

func (t *task) Resume() {
	s := t.s
	s.mu.Lock()
	if !t.active || !t.paused || s.closed || (t.mode == after && t.triggered) {
		s.mu.Unlock()
		return
	}
	t.paused = false
	t.armLocked()
	event := t.eventLocked(Resumed, nil, 0)
	s.mu.Unlock()
	s.emit(event)
}

func (s *scheduler) Touch() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	var events []Event
	for _, t := range s.tasks {
		if t.mode != idle || !t.active || t.paused {
			continue
		}
		t.armLocked()
		events = append(events, t.eventLocked(Scheduled, nil, 0))
	}
	s.mu.Unlock()
	s.emit(events...)
}

func (s *scheduler) Close() error {
	s.mu.Lock()
	var events []Event
	if !s.closed {
		s.closed = true
		for _, t := range s.tasks {
			events = append(events, t.cancelLocked())
		}
	}
	s.mu.Unlock()
	s.emit(events...)
	s.running.Wait()
	return nil
}
