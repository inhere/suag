package session

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestService_ContinueTurnRejectsWhenLatestWaitingRunCannotBeLoaded(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	now := time.Date(2026, 4, 19, 11, 0, 0, 0, time.UTC)

	waitingRunner := &fakeRunner{
		result: RuntimeResult{
			Summary:   "await_resume",
			WaitState: RunInputRequired,
		},
		emitEvents: []RuntimeEvent{
			{
				EventType: EventRunStatus,
				Payload: map[string]any{
					"state": string(RunInputRequired),
				},
			},
		},
	}
	svc := NewService(store, bus, waitingRunner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	firstResult, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_continue_state_gap",
		AgentID:       "default",
		UserMessage:   "need more input",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	if _, err := store.db.ExecContext(ctx, `DELETE FROM runs WHERE run_id = ?`, firstResult.RunID); err != nil {
		t.Fatalf("delete waiting run error = %v", err)
	}

	_, err = svc.ContinueTurn(ctx, ContinueTurnInput{
		SessionID:     "sess_continue_state_gap",
		AgentID:       "default",
		Message:       "continue anyway",
		InitiatorType: InitiatorUser,
	})
	if err == nil {
		t.Fatal("ContinueTurn() error = nil, want missing-latest-run rejection")
	}
	if got := err.Error(); got != "latest waiting run is unavailable; start a new turn or reissue the task" {
		t.Fatalf("ContinueTurn() error = %q, want missing-latest-run rejection", got)
	}
}

type continueStateStore struct {
	Repository
	getRunErr error
}

func (s *continueStateStore) GetRun(ctx context.Context, runID string) (Run, error) {
	if s.getRunErr != nil {
		return Run{}, s.getRunErr
	}
	return s.Repository.GetRun(ctx, runID)
}

func TestService_ContinueTurnReturnsLatestRunLoadError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	baseStore := newServiceTestStore(t)
	bus := newFakePublisher()
	now := time.Date(2026, 4, 19, 11, 5, 0, 0, time.UTC)

	waitingRunner := &fakeRunner{
		result: RuntimeResult{
			Summary:   "await_resume",
			WaitState: RunInputRequired,
		},
		emitEvents: []RuntimeEvent{
			{
				EventType: EventRunStatus,
				Payload: map[string]any{
					"state": string(RunInputRequired),
				},
			},
		},
	}
	svc := NewService(baseStore, bus, waitingRunner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	_, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_continue_state_err",
		AgentID:       "default",
		UserMessage:   "need more input",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	svc.repo = &continueStateStore{
		Repository: baseStore,
		getRunErr:  fmt.Errorf("get run: boom"),
	}

	_, err = svc.ContinueTurn(ctx, ContinueTurnInput{
		SessionID:     "sess_continue_state_err",
		AgentID:       "default",
		Message:       "continue anyway",
		InitiatorType: InitiatorUser,
	})
	if err == nil {
		t.Fatal("ContinueTurn() error = nil, want latest-run load failure")
	}
	if got := err.Error(); got != "load latest run: get run: boom" {
		t.Fatalf("ContinueTurn() error = %q, want wrapped latest-run load failure", got)
	}
}

func TestService_ContinueTurnRejectsSessionWithoutTurns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	now := time.Date(2026, 4, 19, 11, 10, 0, 0, time.UTC)

	if err := store.CreateSession(ctx, Session{
		SessionID: "sess_continue_empty",
		Title:     "empty session",
		Status:    SessionOpen,
		CreatedAt: now,
		UpdatedAt: now,
		Metadata:  map[string]any{},
	}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	svc := NewService(store, bus, &fakeRunner{}, ServiceConfig{
		Now: func() time.Time { return now },
	})

	_, err := svc.ContinueTurn(ctx, ContinueTurnInput{
		SessionID:     "sess_continue_empty",
		AgentID:       "default",
		Message:       "continue anyway",
		InitiatorType: InitiatorUser,
	})
	if err == nil {
		t.Fatal("ContinueTurn() error = nil, want empty-session rejection")
	}
	if got := err.Error(); got != "session is not waiting for continue_turn" {
		t.Fatalf("ContinueTurn() error = %q, want empty-session rejection", got)
	}
}

func TestService_BuildContinueRuntimeContextRejectsSessionWithoutTurns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	now := time.Date(2026, 4, 19, 11, 15, 0, 0, time.UTC)

	if err := store.CreateSession(ctx, Session{
		SessionID: "sess_continue_context_empty",
		Title:     "empty session",
		Status:    SessionOpen,
		CreatedAt: now,
		UpdatedAt: now,
		Metadata:  map[string]any{},
	}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	svc := NewService(store, nil, nil, ServiceConfig{
		Now: func() time.Time { return now },
	})

	_, err := svc.buildContinueRuntimeContext(ctx, "sess_continue_context_empty", "default", "continue anyway")
	if err == nil {
		t.Fatal("buildContinueRuntimeContext() error = nil, want empty-session rejection")
	}
	if got := err.Error(); got != "session is not waiting for continue_turn" {
		t.Fatalf("buildContinueRuntimeContext() error = %q, want empty-session rejection", got)
	}
}

func TestService_BuildContinueRuntimeContextRejectsMissingLatestWaitingRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	now := time.Date(2026, 4, 19, 11, 20, 0, 0, time.UTC)

	waitingRunner := &fakeRunner{
		result: RuntimeResult{
			Summary:   "await_resume",
			WaitState: RunInputRequired,
		},
		emitEvents: []RuntimeEvent{
			{
				EventType: EventRunStatus,
				Payload: map[string]any{
					"state": string(RunInputRequired),
				},
			},
		},
	}
	svc := NewService(store, bus, waitingRunner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	firstResult, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_continue_context_gap",
		AgentID:       "default",
		UserMessage:   "need more input",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	if _, err := store.db.ExecContext(ctx, `DELETE FROM runs WHERE run_id = ?`, firstResult.RunID); err != nil {
		t.Fatalf("delete waiting run error = %v", err)
	}

	_, err = svc.buildContinueRuntimeContext(ctx, "sess_continue_context_gap", "default", "continue anyway")
	if err == nil {
		t.Fatal("buildContinueRuntimeContext() error = nil, want missing-latest-run rejection")
	}
	if got := err.Error(); got != "latest waiting run is unavailable; start a new turn or reissue the task" {
		t.Fatalf("buildContinueRuntimeContext() error = %q, want missing-latest-run rejection", got)
	}
}
