package session

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestSQLiteStore_RequiresMigratedSchema(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "session.db")
	_, err := NewSQLiteStore(dbPath)
	if err == nil {
		t.Fatal("NewSQLiteStore() error = nil, want missing schema error")
	}
	if !strings.Contains(err.Error(), "missing required tables") {
		t.Fatalf("NewSQLiteStore() error = %v, want missing required tables", err)
	}
}

func TestSQLiteStore_CreateSessionTurnRunAndEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)

	now := time.Date(2026, 4, 11, 10, 0, 0, 0, time.UTC)
	sessionModel := Session{
		SessionID:  "sess_001",
		Title:      "test session",
		Status:     SessionOpen,
		CreatedAt:  now,
		UpdatedAt:  now,
		LastTurnID: "",
	}
	turnModel := Turn{
		TurnID:        "turn_001",
		SessionID:     sessionModel.SessionID,
		InitiatorType: InitiatorUser,
		Status:        TurnRunning,
		InputSummary:  "implement feature x",
		CreatedAt:     now,
		PrimaryRunID:  "run_001",
	}
	runModel := Run{
		RunID:     "run_001",
		SessionID: sessionModel.SessionID,
		TurnID:    turnModel.TurnID,
		AgentID:   "default",
		State:     RunQueued,
		StartedAt: now,
	}
	eventModel := RunEvent{
		EventID:   "evt_001",
		SessionID: sessionModel.SessionID,
		TurnID:    turnModel.TurnID,
		RunID:     runModel.RunID,
		AgentID:   runModel.AgentID,
		EventType: "run.queued",
		Sequence:  1,
		Source:    "host.session",
		CreatedAt: now,
	}

	if err := store.CreateSession(ctx, sessionModel); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := store.CreateTurn(ctx, turnModel); err != nil {
		t.Fatalf("CreateTurn() error = %v", err)
	}
	if err := store.CreateRun(ctx, runModel); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	storedEvent, err := store.AppendEvent(ctx, eventModel)
	if err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	if storedEvent.Sequence != 1 {
		t.Fatalf("AppendEvent().Sequence = %d, want %d", storedEvent.Sequence, 1)
	}

	gotSession, err := store.GetSession(ctx, sessionModel.SessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if gotSession.Status != SessionOpen {
		t.Fatalf("GetSession().Status = %q, want %q", gotSession.Status, SessionOpen)
	}

	events, err := store.ListEventsBySession(ctx, sessionModel.SessionID, 0)
	if err != nil {
		t.Fatalf("ListEventsBySession() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("ListEventsBySession() len = %d, want 1", len(events))
	}
	if events[0].EventType != "run.queued" {
		t.Fatalf("ListEventsBySession()[0].EventType = %q, want %q", events[0].EventType, "run.queued")
	}
}

func TestSQLiteStore_AppendEventAssignsSessionWideIncreasingSequence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	now := time.Date(2026, 4, 14, 10, 0, 0, 0, time.UTC)

	if err := store.CreateSession(ctx, Session{
		SessionID: "sess_seq",
		Title:     "sequence session",
		Status:    SessionOpen,
		CreatedAt: now,
		UpdatedAt: now,
		Metadata:  map[string]any{},
	}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	firstEvent, err := store.AppendEvent(ctx, RunEvent{
		EventID:   "evt_seq_001",
		SessionID: "sess_seq",
		TurnID:    "turn_seq_001",
		RunID:     "run_seq_001",
		AgentID:   "default",
		EventType: EventRunQueued,
		Sequence:  99,
		Source:    "test",
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("AppendEvent() first error = %v", err)
	}

	secondEvent, err := store.AppendEvent(ctx, RunEvent{
		EventID:   "evt_seq_002",
		SessionID: "sess_seq",
		TurnID:    "turn_seq_002",
		RunID:     "run_seq_002",
		AgentID:   "default",
		EventType: EventRunStarted,
		Sequence:  1,
		Source:    "test",
		CreatedAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("AppendEvent() second error = %v", err)
	}

	if firstEvent.Sequence != 1 {
		t.Fatalf("first AppendEvent().Sequence = %d, want %d", firstEvent.Sequence, 1)
	}
	if secondEvent.Sequence != 2 {
		t.Fatalf("second AppendEvent().Sequence = %d, want %d", secondEvent.Sequence, 2)
	}

	events, err := store.ListEventsBySession(ctx, "sess_seq", 1)
	if err != nil {
		t.Fatalf("ListEventsBySession() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("ListEventsBySession() len = %d, want 1", len(events))
	}
	if events[0].EventID != "evt_seq_002" {
		t.Fatalf("ListEventsBySession()[0].EventID = %q, want %q", events[0].EventID, "evt_seq_002")
	}
}

func TestSQLiteStore_ListSessionsReturnsLatestFirst(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	now := time.Date(2026, 4, 13, 11, 0, 0, 0, time.UTC)

	if err := store.CreateSession(ctx, Session{
		SessionID: "sess_001",
		Title:     "older session",
		Status:    SessionOpen,
		CreatedAt: now,
		UpdatedAt: now,
		Metadata:  map[string]any{},
	}); err != nil {
		t.Fatalf("CreateSession() older error = %v", err)
	}
	if err := store.CreateSession(ctx, Session{
		SessionID: "sess_002",
		Title:     "latest session",
		Status:    SessionWaiting,
		CreatedAt: now.Add(time.Minute),
		UpdatedAt: now.Add(2 * time.Minute),
		Metadata:  map[string]any{},
	}); err != nil {
		t.Fatalf("CreateSession() latest error = %v", err)
	}

	sessions, err := store.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("ListSessions() len = %d, want 2", len(sessions))
	}
	if sessions[0].SessionID != "sess_002" {
		t.Fatalf("ListSessions()[0].SessionID = %q, want %q", sessions[0].SessionID, "sess_002")
	}
	if sessions[1].SessionID != "sess_001" {
		t.Fatalf("ListSessions()[1].SessionID = %q, want %q", sessions[1].SessionID, "sess_001")
	}
}

func TestSQLiteStore_CreateUpdateAndListExternalTasks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	now := time.Date(2026, 4, 11, 18, 0, 0, 0, time.UTC)

	externalTask := ExternalTask{
		ExternalTaskID:  "ext_001",
		SessionID:       "sess_001",
		TurnID:          "turn_001",
		RunID:           "run_001",
		Adapter:         "codex",
		RemoteAgentName: "codex-cli",
		RemoteSessionID: "remote_sess_001",
		RemoteTaskID:    "remote_task_001",
		State:           "running",
		LastHeartbeatAt: now,
		ResultSummary:   "",
		Metadata: map[string]any{
			"workspace": "workspace/demo",
		},
	}

	if err := store.CreateExternalTask(ctx, externalTask); err != nil {
		t.Fatalf("CreateExternalTask() error = %v", err)
	}

	externalTask.State = "completed"
	externalTask.ResultSummary = "delegated task completed"
	externalTask.LastHeartbeatAt = now.Add(2 * time.Minute)
	externalTask.Metadata["artifacts"] = []any{"artifact://summary"}

	if err := store.UpdateExternalTask(ctx, externalTask); err != nil {
		t.Fatalf("UpdateExternalTask() error = %v", err)
	}

	tasks, err := store.ListExternalTasksByRun(ctx, "run_001")
	if err != nil {
		t.Fatalf("ListExternalTasksByRun() error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("ListExternalTasksByRun() len = %d, want 1", len(tasks))
	}
	if tasks[0].State != "completed" {
		t.Fatalf("ListExternalTasksByRun()[0].State = %q, want %q", tasks[0].State, "completed")
	}
	if tasks[0].ResultSummary != "delegated task completed" {
		t.Fatalf("ListExternalTasksByRun()[0].ResultSummary = %q, want %q", tasks[0].ResultSummary, "delegated task completed")
	}

	sessionTasks, err := store.ListExternalTasksBySession(ctx, "sess_001")
	if err != nil {
		t.Fatalf("ListExternalTasksBySession() error = %v", err)
	}
	if len(sessionTasks) != 1 {
		t.Fatalf("ListExternalTasksBySession() len = %d, want 1", len(sessionTasks))
	}
	if sessionTasks[0].ExternalTaskID != "ext_001" {
		t.Fatalf("ListExternalTasksBySession()[0].ExternalTaskID = %q, want %q", sessionTasks[0].ExternalTaskID, "ext_001")
	}
}

func TestSQLiteStore_UpdateTurnStatusAndRunLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	now := time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC)

	if err := store.CreateSession(ctx, Session{
		SessionID: "sess_010",
		Title:     "lifecycle session",
		Status:    SessionRunning,
		CreatedAt: now,
		UpdatedAt: now,
		Metadata:  map[string]any{},
	}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := store.CreateTurn(ctx, Turn{
		TurnID:        "turn_010",
		SessionID:     "sess_010",
		InitiatorType: InitiatorUser,
		Status:        TurnRunning,
		InputSummary:  "finish task",
		PrimaryRunID:  "run_010",
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("CreateTurn() error = %v", err)
	}
	if err := store.CreateRun(ctx, Run{
		RunID:     "run_010",
		SessionID: "sess_010",
		TurnID:    "turn_010",
		AgentID:   "default",
		State:     RunQueued,
		StartedAt: now,
	}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	finishedAt := now.Add(2 * time.Minute)
	if err := store.UpdateTurnStatus(ctx, "turn_010", TurnCompleted, &finishedAt); err != nil {
		t.Fatalf("UpdateTurnStatus() error = %v", err)
	}
	if err := store.UpdateRunLifecycle(ctx, "run_010", RunCompleted, "done", "", &finishedAt); err != nil {
		t.Fatalf("UpdateRunLifecycle() error = %v", err)
	}

	turnRow := store.db.QueryRowContext(ctx, `
		SELECT status, completed_at
		FROM turns
		WHERE turn_id = ?
	`, "turn_010")
	var turnStatus, completedAt string
	if err := turnRow.Scan(&turnStatus, &completedAt); err != nil {
		t.Fatalf("query turn lifecycle error = %v", err)
	}
	if turnStatus != string(TurnCompleted) {
		t.Fatalf("persisted turn status = %q, want %q", turnStatus, TurnCompleted)
	}
	if completedAt != finishedAt.Format(time.RFC3339Nano) {
		t.Fatalf("persisted turn completed_at = %q, want %q", completedAt, finishedAt.Format(time.RFC3339Nano))
	}

	runRow := store.db.QueryRowContext(ctx, `
		SELECT state, summary, failure_code, ended_at
		FROM runs
		WHERE run_id = ?
	`, "run_010")
	var runState, runSummary, failureCode, endedAt string
	if err := runRow.Scan(&runState, &runSummary, &failureCode, &endedAt); err != nil {
		t.Fatalf("query run lifecycle error = %v", err)
	}
	if runState != string(RunCompleted) {
		t.Fatalf("persisted run state = %q, want %q", runState, RunCompleted)
	}
	if runSummary != "done" {
		t.Fatalf("persisted run summary = %q, want %q", runSummary, "done")
	}
	if failureCode != "" {
		t.Fatalf("persisted run failure_code = %q, want empty", failureCode)
	}
	if endedAt != finishedAt.Format(time.RFC3339Nano) {
		t.Fatalf("persisted run ended_at = %q, want %q", endedAt, finishedAt.Format(time.RFC3339Nano))
	}
}

func TestSQLiteStore_CreateAndListRecapsByTurn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	now := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)

	recap := Recap{
		RecapID:   "recap_001",
		SessionID: "sess_001",
		TurnID:    "turn_001",
		RunID:     "run_001",
		Kind:      "turn_recap",
		Target:    "assistant",
		Content:   "final summary",
		CreatedAt: now,
		Metadata: map[string]any{
			"phase": "completed",
		},
	}

	if err := store.CreateRecap(ctx, recap); err != nil {
		t.Fatalf("CreateRecap() error = %v", err)
	}

	recaps, err := store.ListRecapsByTurn(ctx, "sess_001", "turn_001")
	if err != nil {
		t.Fatalf("ListRecapsByTurn() error = %v", err)
	}
	if len(recaps) != 1 {
		t.Fatalf("ListRecapsByTurn() len = %d, want 1", len(recaps))
	}
	if recaps[0].RecapID != "recap_001" {
		t.Fatalf("ListRecapsByTurn()[0].RecapID = %q, want %q", recaps[0].RecapID, "recap_001")
	}
	if recaps[0].Content != "final summary" {
		t.Fatalf("ListRecapsByTurn()[0].Content = %q, want %q", recaps[0].Content, "final summary")
	}
	if recaps[0].Metadata["phase"] != "completed" {
		t.Fatalf("ListRecapsByTurn()[0].Metadata[phase] = %v, want %q", recaps[0].Metadata["phase"], "completed")
	}
}

func TestSQLiteStore_CreateAndListMemoryCandidatesByTurn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	now := time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC)

	candidate := MemoryCandidate{
		CandidateID:   "mcand_001",
		SessionID:     "sess_001",
		TurnID:        "turn_001",
		RunID:         "run_001",
		SourceRecapID: "recap_001",
		Target:        "MEMORY",
		Kind:          "turn_learning",
		Content:       "Prefer stable host state contracts for waiting turns.",
		Score:         0.8,
		Status:        "draft",
		CreatedAt:     now,
		Metadata: map[string]any{
			"phase": "completed",
		},
	}

	if err := store.CreateMemoryCandidate(ctx, candidate); err != nil {
		t.Fatalf("CreateMemoryCandidate() error = %v", err)
	}

	candidates, err := store.ListMemoryCandidatesByTurn(ctx, "sess_001", "turn_001")
	if err != nil {
		t.Fatalf("ListMemoryCandidatesByTurn() error = %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("ListMemoryCandidatesByTurn() len = %d, want 1", len(candidates))
	}
	if candidates[0].CandidateID != "mcand_001" {
		t.Fatalf("ListMemoryCandidatesByTurn()[0].CandidateID = %q, want %q", candidates[0].CandidateID, "mcand_001")
	}
	if candidates[0].SourceRecapID != "recap_001" {
		t.Fatalf("ListMemoryCandidatesByTurn()[0].SourceRecapID = %q, want %q", candidates[0].SourceRecapID, "recap_001")
	}
	if candidates[0].Target != "MEMORY" {
		t.Fatalf("ListMemoryCandidatesByTurn()[0].Target = %q, want %q", candidates[0].Target, "MEMORY")
	}
	if candidates[0].Metadata["phase"] != "completed" {
		t.Fatalf("ListMemoryCandidatesByTurn()[0].Metadata[phase] = %v, want %q", candidates[0].Metadata["phase"], "completed")
	}
}

func newTestStore(t *testing.T) *SQLiteStore {
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

func createMigratedTestDB(t *testing.T) string {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "session.db")
	applyHostSessionMigration(t, dbPath)
	return dbPath
}

func applyHostSessionMigration(t *testing.T, dbPath string) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatalf("db.Close() error = %v", err)
		}
	}()

	for _, upSQL := range readHostSessionMigrationUps(t) {
		if _, err := db.Exec(upSQL); err != nil {
			t.Fatalf("db.Exec() migration error = %v", err)
		}
	}
}

func readHostSessionMigrationUps(t *testing.T) []string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}

	migrationDir := filepath.Join(
		filepath.Dir(file),
		"..", "..", "..",
		"db", "migrations",
	)
	entries, err := filepath.Glob(filepath.Join(migrationDir, "*.sql"))
	if err != nil {
		t.Fatalf("filepath.Glob() error = %v", err)
	}
	sort.Strings(entries)
	if len(entries) == 0 {
		t.Fatalf("no migration files found in %s", migrationDir)
	}

	ups := make([]string, 0, len(entries))
	for _, migrationPath := range entries {
		content, err := os.ReadFile(migrationPath)
		if err != nil {
			t.Fatalf("os.ReadFile() error = %v", err)
		}

		parts := strings.Split(string(content), "-- Migrate:DOWN")
		if len(parts) != 2 {
			t.Fatalf("unexpected migration format in %s", migrationPath)
		}

		upSQL := strings.TrimSpace(strings.TrimPrefix(parts[0], "-- Migrate:UP"))
		if upSQL == "" {
			t.Fatalf("empty migration up section in %s", migrationPath)
		}
		ups = append(ups, upSQL)
	}
	return ups
}
