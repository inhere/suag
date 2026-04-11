package session

import (
	"context"
	"testing"
	"time"
)

func TestService_StartTurnCreatesQueuedRunAndPublishesEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		result: RuntimeResult{
			Summary: "run-output",
		},
	}
	now := time.Date(2026, 4, 11, 11, 0, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	result, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_001",
		AgentID:       "default",
		UserMessage:   "implement feature x",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	if result.SessionID != "sess_001" {
		t.Fatalf("StartTurn().SessionID = %q, want %q", result.SessionID, "sess_001")
	}
	if result.TurnID == "" {
		t.Fatal("StartTurn().TurnID is empty")
	}
	if result.RunID == "" {
		t.Fatal("StartTurn().RunID is empty")
	}
	if len(bus.events) == 0 {
		t.Fatal("publisher received no events")
	}
	if bus.events[0].EventType != EventRunQueued {
		t.Fatalf("first published event type = %q, want %q", bus.events[0].EventType, EventRunQueued)
	}
	if runner.lastRequest.SessionID != "sess_001" {
		t.Fatalf("runner request SessionID = %q, want %q", runner.lastRequest.SessionID, "sess_001")
	}
	if runner.lastRequest.TurnID != result.TurnID {
		t.Fatalf("runner request TurnID = %q, want %q", runner.lastRequest.TurnID, result.TurnID)
	}
	if runner.lastRequest.RunID != result.RunID {
		t.Fatalf("runner request RunID = %q, want %q", runner.lastRequest.RunID, result.RunID)
	}

	gotSession, err := store.GetSession(ctx, "sess_001")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if gotSession.LastTurnID != result.TurnID {
		t.Fatalf("GetSession().LastTurnID = %q, want %q", gotSession.LastTurnID, result.TurnID)
	}

	events, err := store.ListEventsBySession(ctx, "sess_001", 0)
	if err != nil {
		t.Fatalf("ListEventsBySession() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("ListEventsBySession() len = %d, want 1", len(events))
	}
	if events[0].RunID != result.RunID {
		t.Fatalf("ListEventsBySession()[0].RunID = %q, want %q", events[0].RunID, result.RunID)
	}
}

func TestService_ContinueTurnCreatesNewTurnForExistingSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	now := time.Date(2026, 4, 11, 11, 30, 0, 0, time.UTC)

	if err := store.CreateSession(ctx, Session{
		SessionID: "sess_001",
		Title:     "existing session",
		Status:    SessionOpen,
		CreatedAt: now,
		UpdatedAt: now,
		Metadata:  map[string]any{},
	}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	bus := newFakePublisher()
	runner := &fakeRunner{
		result: RuntimeResult{Summary: "continued"},
	}
	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now.Add(5 * time.Minute) },
	})

	result, err := svc.ContinueTurn(ctx, ContinueTurnInput{
		SessionID:     "sess_001",
		AgentID:       "default",
		Message:       "please continue",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("ContinueTurn() error = %v", err)
	}
	if result.SessionID != "sess_001" {
		t.Fatalf("ContinueTurn().SessionID = %q, want %q", result.SessionID, "sess_001")
	}
	if result.TurnID == "" {
		t.Fatal("ContinueTurn().TurnID is empty")
	}
	if runner.lastRequest.Instruction != "please continue" {
		t.Fatalf("runner request Instruction = %q, want %q", runner.lastRequest.Instruction, "please continue")
	}

	gotSession, err := store.GetSession(ctx, "sess_001")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if gotSession.LastTurnID != result.TurnID {
		t.Fatalf("GetSession().LastTurnID = %q, want %q", gotSession.LastTurnID, result.TurnID)
	}
	if len(bus.events) != 1 {
		t.Fatalf("published events len = %d, want 1", len(bus.events))
	}
}

type fakeRunner struct {
	result      RuntimeResult
	err         error
	lastRequest RuntimeRequest
}

func (f *fakeRunner) Run(_ context.Context, in RuntimeRequest) (RuntimeResult, error) {
	f.lastRequest = in
	return f.result, f.err
}

type fakePublisher struct {
	events []RunEvent
}

func newFakePublisher() *fakePublisher {
	return &fakePublisher{events: make([]RunEvent, 0, 4)}
}

func (f *fakePublisher) PublishSessionEvent(_ context.Context, event RunEvent) error {
	f.events = append(f.events, event)
	return nil
}

func newServiceTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dbPath := createMigratedTestDB(t)
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return store
}
