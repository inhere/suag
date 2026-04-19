package session

import (
	"context"
	"testing"
	"time"
)

func TestService_StartTurnDoesNotAppendDuplicateCanceledEventWhenRuntimeAlreadyEmittedOne(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	now := time.Date(2026, 4, 19, 10, 30, 0, 0, time.UTC)
	runner := newCanceledEventBlockingRunner(RuntimeEvent{
		EventType: EventRunCanceled,
		Payload: map[string]any{
			"reason": "runtime canceled",
		},
	})

	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	startDone := make(chan error, 1)
	go func() {
		_, err := svc.StartTurn(ctx, StartTurnInput{
			SessionID:     "sess_canceled_once",
			AgentID:       "default",
			UserMessage:   "cancel once",
			InitiatorType: InitiatorUser,
		})
		startDone <- err
	}()

	runner.WaitUntilCanceledEventEmitted(t)

	if err := svc.StopTurn(ctx, StopTurnInput{
		SessionID: "sess_canceled_once",
		TurnID:    runner.LastRequest().TurnID,
		Reason:    "stopped by operator",
	}); err != nil {
		t.Fatalf("StopTurn() error = %v", err)
	}

	select {
	case err := <-startDone:
		if err != nil {
			t.Fatalf("StartTurn() error after stop = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("StartTurn() did not finish after stop")
	}

	events, err := store.ListEventsBySession(ctx, "sess_canceled_once", 0)
	if err != nil {
		t.Fatalf("ListEventsBySession() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("ListEventsBySession() len = %d, want 3", len(events))
	}
	if events[2].EventType != EventRunCanceled {
		t.Fatalf("events[2].EventType = %q, want %q", events[2].EventType, EventRunCanceled)
	}

	canceledCount := 0
	for _, event := range events {
		if event.EventType == EventRunCanceled {
			canceledCount++
		}
	}
	if canceledCount != 1 {
		t.Fatalf("canceled event count = %d, want 1", canceledCount)
	}
}

func TestService_StartTurnPreservesRuntimeCanceledTerminalState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	now := time.Date(2026, 4, 19, 10, 45, 0, 0, time.UTC)

	runner := &fakeRunner{
		result: RuntimeResult{},
		emitEvents: []RuntimeEvent{
			{
				EventType: EventRunCanceled,
				Payload: map[string]any{
					"reason": "runtime canceled directly",
				},
			},
		},
	}
	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	result, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_runtime_canceled",
		AgentID:       "default",
		UserMessage:   "runtime cancels itself",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	events, err := store.ListEventsBySession(ctx, "sess_runtime_canceled", 0)
	if err != nil {
		t.Fatalf("ListEventsBySession() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("ListEventsBySession() len = %d, want 3", len(events))
	}
	if events[2].EventType != EventRunCanceled {
		t.Fatalf("events[2].EventType = %q, want %q", events[2].EventType, EventRunCanceled)
	}

	finishedCount := 0
	for _, event := range events {
		if event.EventType == EventRunFinished {
			finishedCount++
		}
	}
	if finishedCount != 0 {
		t.Fatalf("finished event count = %d, want 0", finishedCount)
	}

	runModel, err := store.GetRun(ctx, result.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if runModel.State != RunCanceled {
		t.Fatalf("persisted run state = %q, want %q", runModel.State, RunCanceled)
	}

	row := store.db.QueryRowContext(ctx, `
		SELECT status
		FROM turns
		WHERE turn_id = ?
	`, result.TurnID)
	var turnStatus string
	if err := row.Scan(&turnStatus); err != nil {
		t.Fatalf("query turn status error = %v", err)
	}
	if turnStatus != string(TurnCanceled) {
		t.Fatalf("persisted turn status = %q, want %q", turnStatus, TurnCanceled)
	}

	sessionModel, err := store.GetSession(ctx, result.SessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if sessionModel.Status != SessionOpen {
		t.Fatalf("persisted session status = %q, want %q", sessionModel.Status, SessionOpen)
	}
}

type canceledEventBlockingRunner struct {
	blockingRunner
	canceledEvent RuntimeEvent
	emittedCh     chan struct{}
}

func newCanceledEventBlockingRunner(event RuntimeEvent) *canceledEventBlockingRunner {
	return &canceledEventBlockingRunner{
		blockingRunner: blockingRunner{
			startedCh: make(chan struct{}),
		},
		canceledEvent: event,
		emittedCh:     make(chan struct{}),
	}
}

func (r *canceledEventBlockingRunner) Run(ctx context.Context, in RuntimeRequest) (RuntimeResult, error) {
	r.mu.Lock()
	r.lastRequest = in
	r.mu.Unlock()

	select {
	case <-r.startedCh:
	default:
		close(r.startedCh)
	}

	if in.Emit != nil {
		if err := in.Emit(r.canceledEvent); err != nil {
			return RuntimeResult{}, err
		}
	}

	select {
	case <-r.emittedCh:
	default:
		close(r.emittedCh)
	}

	<-ctx.Done()
	return RuntimeResult{}, ctx.Err()
}

func (r *canceledEventBlockingRunner) WaitUntilCanceledEventEmitted(t *testing.T) {
	t.Helper()

	select {
	case <-r.emittedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled event blocking runner did not emit canceled event")
	}
}
