package session

import (
	"context"
	"errors"
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
	if len(bus.events) != 3 {
		t.Fatalf("published events len = %d, want 3", len(bus.events))
	}
	if bus.events[1].EventType != EventRunStarted {
		t.Fatalf("second published event type = %q, want %q", bus.events[1].EventType, EventRunStarted)
	}
	if bus.events[2].EventType != EventRunFinished {
		t.Fatalf("third published event type = %q, want %q", bus.events[2].EventType, EventRunFinished)
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
	if len(events) != 3 {
		t.Fatalf("ListEventsBySession() len = %d, want 3", len(events))
	}
	if events[2].RunID != result.RunID {
		t.Fatalf("ListEventsBySession()[2].RunID = %q, want %q", events[2].RunID, result.RunID)
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
	if len(bus.events) != 3 {
		t.Fatalf("published events len = %d, want 3", len(bus.events))
	}
}

func TestService_ContinueTurnPassesResumeExternalTasksFromPreviousTurn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	firstRunner := &fakeRunner{
		result: RuntimeResult{
			Summary: "delegated",
			ExternalTask: &ExternalTask{
				Adapter:         "codex",
				RemoteAgentName: "codex-cli",
				RemoteSessionID: "thread_001",
				RemoteTaskID:    "turn_remote_001",
				State:           "completed",
				Metadata: map[string]any{
					"protocol": "codex_app_server",
				},
			},
		},
	}
	now := time.Date(2026, 4, 12, 11, 0, 0, 0, time.UTC)
	svc := NewService(store, bus, firstRunner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	firstResult, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_001",
		AgentID:       "default",
		UserMessage:   "delegate first turn",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	secondRunner := &fakeRunner{
		result: RuntimeResult{Summary: "continued"},
	}
	svc.runner = secondRunner
	svc.now = func() time.Time { return now.Add(5 * time.Minute) }

	_, err = svc.ContinueTurn(ctx, ContinueTurnInput{
		SessionID:     "sess_001",
		AgentID:       "default",
		Message:       "please continue with same remote thread",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("ContinueTurn() error = %v", err)
	}

	if secondRunner.lastRequest.Context == nil {
		t.Fatal("runner request Context is nil")
	}
	if secondRunner.lastRequest.Context["resume_turn_id"] != firstResult.TurnID {
		t.Fatalf("runner request Context[resume_turn_id] = %v, want %q", secondRunner.lastRequest.Context["resume_turn_id"], firstResult.TurnID)
	}
	resumeTasks, ok := secondRunner.lastRequest.Context["resume_external_tasks"].([]ExternalTask)
	if !ok {
		t.Fatalf("runner request Context[resume_external_tasks] type = %T, want []ExternalTask", secondRunner.lastRequest.Context["resume_external_tasks"])
	}
	if len(resumeTasks) != 1 {
		t.Fatalf("runner request Context[resume_external_tasks] len = %d, want 1", len(resumeTasks))
	}
	if resumeTasks[0].RemoteSessionID != "thread_001" {
		t.Fatalf("runner request Context[resume_external_tasks][0].RemoteSessionID = %q, want %q", resumeTasks[0].RemoteSessionID, "thread_001")
	}
	if secondRunner.lastRequest.Context["external_task_id"] == nil {
		t.Fatal("runner request Context[external_task_id] is nil")
	}
}

func TestService_StartTurnPublishesFailedLifecycleWhenRunnerFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		err: errors.New("runner failed"),
	}
	now := time.Date(2026, 4, 11, 11, 10, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	_, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_002",
		AgentID:       "default",
		UserMessage:   "implement broken feature",
		InitiatorType: InitiatorUser,
	})
	if err == nil {
		t.Fatal("StartTurn() error = nil, want runner failure")
	}
	if len(bus.events) != 3 {
		t.Fatalf("published events len = %d, want 3", len(bus.events))
	}
	if bus.events[2].EventType != EventRunFailed {
		t.Fatalf("third published event type = %q, want %q", bus.events[2].EventType, EventRunFailed)
	}
}

func TestService_StartTurnPublishesRuntimeEventsFromRunner(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		result: RuntimeResult{
			Summary: "done",
		},
		emitEvents: []RuntimeEvent{
			{
				EventType: EventRunExternalActivity,
				Payload: map[string]any{
					"kind": "item_started",
				},
			},
			{
				EventType: EventRunExternalMessageDelta,
				Payload: map[string]any{
					"messageId":    "turn_remote_001:item_001",
					"delta":        "hello",
					"isFirstChunk": true,
				},
			},
		},
	}
	now := time.Date(2026, 4, 12, 13, 0, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	result, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_100",
		AgentID:       "default",
		UserMessage:   "emit runtime events",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	events, err := store.ListEventsBySession(ctx, "sess_100", 0)
	if err != nil {
		t.Fatalf("ListEventsBySession() error = %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("ListEventsBySession() len = %d, want 5", len(events))
	}
	if events[2].EventType != EventRunExternalActivity {
		t.Fatalf("events[2].EventType = %q, want %q", events[2].EventType, EventRunExternalActivity)
	}
	if events[2].Payload["externalTaskId"] == "" {
		t.Fatal("events[2].Payload[externalTaskId] is empty")
	}
	if events[3].EventType != EventRunExternalMessageDelta {
		t.Fatalf("events[3].EventType = %q, want %q", events[3].EventType, EventRunExternalMessageDelta)
	}
	if events[4].EventType != EventRunFinished {
		t.Fatalf("events[4].EventType = %q, want %q", events[4].EventType, EventRunFinished)
	}
	if events[4].RunID != result.RunID {
		t.Fatalf("events[4].RunID = %q, want %q", events[4].RunID, result.RunID)
	}
}

func TestService_StartTurnPersistsCompletedLifecycleState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		result: RuntimeResult{
			Summary: "final summary",
			ExternalTask: &ExternalTask{
				ExternalTaskID:  "ext_complete_001",
				Adapter:         "codex",
				RemoteAgentName: "codex-cli",
				RemoteSessionID: "remote_sess_001",
				RemoteTaskID:    "remote_task_001",
				State:           "completed",
				ResultSummary:   "remote task done",
			},
		},
	}
	now := time.Date(2026, 4, 13, 9, 0, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	result, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_done",
		AgentID:       "default",
		UserMessage:   "finish the task",
		InitiatorType: InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	row := store.db.QueryRowContext(ctx, `
		SELECT state, summary, ended_at
		FROM runs
		WHERE run_id = ?
	`, result.RunID)
	var runState, runSummary, runEndedAt string
	if err := row.Scan(&runState, &runSummary, &runEndedAt); err != nil {
		t.Fatalf("query run state error = %v", err)
	}
	if runState != string(RunCompleted) {
		t.Fatalf("persisted run state = %q, want %q", runState, RunCompleted)
	}
	if runSummary != "final summary" {
		t.Fatalf("persisted run summary = %q, want %q", runSummary, "final summary")
	}
	if runEndedAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("persisted run ended_at = %q, want %q", runEndedAt, now.Format(time.RFC3339Nano))
	}

	sessionRow := store.db.QueryRowContext(ctx, `
		SELECT status
		FROM sessions
		WHERE session_id = ?
	`, result.SessionID)
	var sessionStatus string
	if err := sessionRow.Scan(&sessionStatus); err != nil {
		t.Fatalf("query session status error = %v", err)
	}
	if sessionStatus != string(SessionWaiting) {
		t.Fatalf("persisted session status = %q, want %q", sessionStatus, SessionWaiting)
	}

	taskRow := store.db.QueryRowContext(ctx, `
		SELECT state, result_summary
		FROM external_tasks
		WHERE external_task_id = ?
	`, "ext_complete_001")
	var taskState, taskSummary string
	if err := taskRow.Scan(&taskState, &taskSummary); err != nil {
		t.Fatalf("query external task error = %v", err)
	}
	if taskState != "completed" {
		t.Fatalf("persisted external task state = %q, want %q", taskState, "completed")
	}
	if taskSummary != "remote task done" {
		t.Fatalf("persisted external task result summary = %q, want %q", taskSummary, "remote task done")
	}
}

func TestService_StartTurnPersistsFailedLifecycleState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newServiceTestStore(t)
	bus := newFakePublisher()
	runner := &fakeRunner{
		err: errors.New("runner failed"),
	}
	now := time.Date(2026, 4, 13, 9, 30, 0, 0, time.UTC)

	svc := NewService(store, bus, runner, ServiceConfig{
		Now: func() time.Time { return now },
	})

	_, err := svc.StartTurn(ctx, StartTurnInput{
		SessionID:     "sess_failed",
		AgentID:       "default",
		UserMessage:   "break the task",
		InitiatorType: InitiatorUser,
	})
	if err == nil {
		t.Fatal("StartTurn() error = nil, want runner failure")
	}

	row := store.db.QueryRowContext(ctx, `
		SELECT run_id, state, summary, ended_at
		FROM runs
		WHERE session_id = ?
		ORDER BY started_at DESC
		LIMIT 1
	`, "sess_failed")
	var runID, runState, runSummary, runEndedAt string
	if err := row.Scan(&runID, &runState, &runSummary, &runEndedAt); err != nil {
		t.Fatalf("query failed run state error = %v", err)
	}
	if runID == "" {
		t.Fatal("persisted failed run id is empty")
	}
	if runState != string(RunFailed) {
		t.Fatalf("persisted failed run state = %q, want %q", runState, RunFailed)
	}
	if runSummary != "" {
		t.Fatalf("persisted failed run summary = %q, want empty", runSummary)
	}
	if runEndedAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("persisted failed run ended_at = %q, want %q", runEndedAt, now.Format(time.RFC3339Nano))
	}

	sessionRow := store.db.QueryRowContext(ctx, `
		SELECT status
		FROM sessions
		WHERE session_id = ?
	`, "sess_failed")
	var sessionStatus string
	if err := sessionRow.Scan(&sessionStatus); err != nil {
		t.Fatalf("query failed session status error = %v", err)
	}
	if sessionStatus != string(SessionWaiting) {
		t.Fatalf("persisted failed session status = %q, want %q", sessionStatus, SessionWaiting)
	}
}

type fakeRunner struct {
	result      RuntimeResult
	err         error
	lastRequest RuntimeRequest
	emitEvents  []RuntimeEvent
}

func (f *fakeRunner) Run(_ context.Context, in RuntimeRequest) (RuntimeResult, error) {
	f.lastRequest = in
	for _, event := range f.emitEvents {
		if in.Emit == nil {
			continue
		}
		if err := in.Emit(event); err != nil {
			return RuntimeResult{}, err
		}
	}
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
