package session

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
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
	if err := store.AppendEvent(ctx, eventModel); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
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

	upSQL := readHostSessionMigrationUp(t)
	if _, err := db.Exec(upSQL); err != nil {
		t.Fatalf("db.Exec() migration error = %v", err)
	}
}

func readHostSessionMigrationUp(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}

	migrationPath := filepath.Join(
		filepath.Dir(file),
		"..", "..", "..",
		"db", "migrations", "20260411-100000-create-host-session-foundation.sql",
	)
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
	return upSQL
}
