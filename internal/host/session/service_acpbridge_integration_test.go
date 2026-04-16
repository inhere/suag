package session_test

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

	"github.com/inhere/axon/internal/host/acpbridge"
	"github.com/inhere/axon/internal/host/session"
	_ "modernc.org/sqlite"
)

func TestService_StartTurnWithACPBridgeRunnerPublishesFinishedLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newIntegrationStore(t)
	bus := newIntegrationPublisher()
	bridge := &fakeACPBridge{
		result: acpbridge.DispatchResult{
			State:   acpbridge.StateCompleted,
			Summary: "completed by codex",
		},
	}
	runner := acpbridge.NewRunnerAdapter(bridge, acpbridge.RunnerAdapterConfig{
		AdapterName: "codex",
		Workspace:   "workspace/demo",
	})

	now := time.Date(2026, 4, 11, 17, 0, 0, 0, time.UTC)
	svc := session.NewService(store, bus, runner, session.ServiceConfig{
		Now: func() time.Time { return now },
	})

	result, err := svc.StartTurn(ctx, session.StartTurnInput{
		SessionID:     "sess_001",
		AgentID:       "default",
		UserMessage:   "delegate to codex",
		InitiatorType: session.InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	if result.RunID == "" {
		t.Fatal("StartTurn().RunID is empty")
	}
	if bridge.lastRequest.RunID != result.RunID {
		t.Fatalf("Dispatch().RunID = %q, want %q", bridge.lastRequest.RunID, result.RunID)
	}
	if bridge.lastRequest.Adapter != "codex" {
		t.Fatalf("Dispatch().Adapter = %q, want %q", bridge.lastRequest.Adapter, "codex")
	}
	if len(bus.events) != 4 {
		t.Fatalf("published events len = %d, want 4", len(bus.events))
	}
	if bus.events[2].EventType != session.EventRunStatus {
		t.Fatalf("published events[2] type = %q, want %q", bus.events[2].EventType, session.EventRunStatus)
	}
	if bus.events[3].EventType != session.EventRunFinished {
		t.Fatalf("last published event type = %q, want %q", bus.events[3].EventType, session.EventRunFinished)
	}
}

func TestService_StartTurnWithACPBridgeRunnerPublishesExternalTaskBeforeFinish(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newIntegrationStore(t)
	bus := newIntegrationPublisher()
	bridge := &fakeACPBridge{
		result: acpbridge.DispatchResult{
			State:           acpbridge.StateCompleted,
			Summary:         "completed by codex",
			RemoteAgentName: "codex-cli",
			RemoteSessionID: "thread_001",
			RemoteTaskID:    "turn_remote_001",
		},
	}
	runner := acpbridge.NewRunnerAdapter(bridge, acpbridge.RunnerAdapterConfig{
		AdapterName: "codex",
		Workspace:   "workspace/demo",
	})

	now := time.Date(2026, 4, 11, 17, 30, 0, 0, time.UTC)
	svc := session.NewService(store, bus, runner, session.ServiceConfig{
		Now: func() time.Time { return now },
	})

	result, err := svc.StartTurn(ctx, session.StartTurnInput{
		SessionID:     "sess_finish_order",
		AgentID:       "default",
		UserMessage:   "delegate to codex",
		InitiatorType: session.InitiatorUser,
	})
	if err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	tasks, err := store.ListExternalTasksByRun(ctx, result.RunID)
	if err != nil {
		t.Fatalf("ListExternalTasksByRun() error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("ListExternalTasksByRun() len = %d, want 1", len(tasks))
	}
	if tasks[0].RemoteSessionID != "thread_001" {
		t.Fatalf("ListExternalTasksByRun()[0].RemoteSessionID = %q, want %q", tasks[0].RemoteSessionID, "thread_001")
	}

	if bus.events[len(bus.events)-1].EventType != session.EventRunFinished {
		t.Fatalf("last published event type = %q, want %q", bus.events[len(bus.events)-1].EventType, session.EventRunFinished)
	}
}

type fakeACPBridge struct {
	lastRequest acpbridge.DispatchRequest
	result      acpbridge.DispatchResult
	err         error
}

func (f *fakeACPBridge) Dispatch(_ context.Context, req acpbridge.DispatchRequest) (acpbridge.DispatchResult, error) {
	f.lastRequest = req
	return f.result, f.err
}

type integrationPublisher struct {
	events []session.RunEvent
}

func newIntegrationPublisher() *integrationPublisher {
	return &integrationPublisher{events: make([]session.RunEvent, 0, 8)}
}

func (p *integrationPublisher) PublishSessionEvent(_ context.Context, event session.RunEvent) error {
	p.events = append(p.events, event)
	return nil
}

func newIntegrationStore(t *testing.T) *session.SQLiteStore {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "session.db")
	applyIntegrationMigration(t, dbPath)

	store, err := session.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("session.NewSQLiteStore() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("store.Close() error = %v", err)
		}
	})
	return store
}

func applyIntegrationMigration(t *testing.T, dbPath string) {
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

	for _, upSQL := range readIntegrationMigrationUps(t) {
		if _, err := db.Exec(upSQL); err != nil {
			t.Fatalf("db.Exec() migration error = %v", err)
		}
	}
}

func readIntegrationMigrationUps(t *testing.T) []string {
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
