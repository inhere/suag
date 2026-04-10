package agentcorestorage_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/inhere/axon/internal/acstore"
	"github.com/inhere/axon/pkg/agentcore"
)

func TestSQLiteStorage_SaveRunAndLoadRun(t *testing.T) {
	t.Parallel()

	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	saved := &agentcore.RunState{
		RunID:    "run-state",
		AgentID:  "agent-1",
		Status:   agentcore.RunRunning,
		Step:     2,
		Messages: []agentcore.Message{{Role: "user", Content: "snapshot-only"}},
		Metadata: map[string]any{"source": "snapshot"},
	}

	if err := store.SaveRun(context.Background(), saved); err != nil {
		t.Fatalf("expected save to succeed, got %v", err)
	}

	loaded, err := store.LoadRun(context.Background(), "run-state")
	if err != nil {
		t.Fatalf("expected load to succeed, got %v", err)
	}
	if loaded.RunID != saved.RunID || loaded.AgentID != saved.AgentID {
		t.Fatalf("expected identifiers to round-trip, got %#v", loaded)
	}
	if len(loaded.Messages) != 1 || loaded.Messages[0].Content != "snapshot-only" {
		t.Fatalf("expected snapshot messages to round-trip, got %#v", loaded.Messages)
	}
	if loaded.Metadata["source"] != "snapshot" {
		t.Fatalf("expected metadata to round-trip, got %#v", loaded.Metadata)
	}
}

func TestSQLiteStorage_AppendAndLoadMessages(t *testing.T) {
	t.Parallel()

	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	want := []agentcore.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world", Metadata: map[string]any{"turn": float64(2)}},
	}

	if err := store.AppendMessages(context.Background(), "run-msgs", want...); err != nil {
		t.Fatalf("expected append to succeed, got %v", err)
	}

	got, err := store.LoadMessages(context.Background(), "run-msgs")
	if err != nil {
		t.Fatalf("expected load messages to succeed, got %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d messages, got %d", len(want), len(got))
	}
	if got[0].Content != "hello" || got[1].Content != "world" {
		t.Fatalf("expected messages in order, got %#v", got)
	}
	if got[1].Metadata["turn"] != float64(2) {
		t.Fatalf("expected metadata to round-trip, got %#v", got[1].Metadata)
	}
}

func TestSQLiteStorage_LoadMessagesTreatsTranscriptAsAuthority(t *testing.T) {
	t.Parallel()

	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	snapshot := &agentcore.RunState{
		RunID:    "run-authority",
		AgentID:  "agent-1",
		Status:   agentcore.RunRunning,
		Messages: []agentcore.Message{{Role: "assistant", Content: "stale-snapshot"}},
	}
	authoritative := []agentcore.Message{
		{Role: "user", Content: "from-log-1"},
		{Role: "assistant", Content: "from-log-2"},
	}

	if err := store.SaveRun(context.Background(), snapshot); err != nil {
		t.Fatalf("expected save to succeed, got %v", err)
	}
	if err := store.AppendMessages(context.Background(), "run-authority", authoritative...); err != nil {
		t.Fatalf("expected append to succeed, got %v", err)
	}

	loadedState, err := store.LoadRun(context.Background(), "run-authority")
	if err != nil {
		t.Fatalf("expected load run to succeed, got %v", err)
	}
	loadedMessages, err := store.LoadMessages(context.Background(), "run-authority")
	if err != nil {
		t.Fatalf("expected load messages to succeed, got %v", err)
	}
	if len(loadedState.Messages) != 1 || loadedState.Messages[0].Content != "stale-snapshot" {
		t.Fatalf("expected snapshot payload to remain independent, got %#v", loadedState.Messages)
	}
	if len(loadedMessages) != 2 || loadedMessages[0].Content != "from-log-1" || loadedMessages[1].Content != "from-log-2" {
		t.Fatalf("expected transcript to remain authoritative, got %#v", loadedMessages)
	}
}

func TestSQLiteStorage_MissingRunReturnsWrappedNotExist(t *testing.T) {
	t.Parallel()

	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	_, err = store.LoadRun(context.Background(), "missing-run")
	assertStorageError(t, err)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected missing run to wrap os.ErrNotExist, got %v", err)
	}

	_, err = store.LoadMessages(context.Background(), "missing-run")
	assertStorageError(t, err)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected missing transcript to wrap os.ErrNotExist, got %v", err)
	}
}

func TestSQLiteStorage_RejectsEmptyRunID(t *testing.T) {
	t.Parallel()

	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	_, err = store.LoadRun(context.Background(), "")
	assertStorageError(t, err)

	_, err = store.LoadMessages(context.Background(), "")
	assertStorageError(t, err)

	err = store.AppendMessages(context.Background(), "", agentcore.Message{Role: "user", Content: "hi"})
	assertStorageError(t, err)

	err = store.SaveRun(context.Background(), &agentcore.RunState{})
	assertStorageError(t, err)
}

func TestSQLiteStorage_CreatesParentDirectory(t *testing.T) {
	t.Parallel()

	dbPath := t.TempDir() + "/nested/agentcore.db"
	store, err := agentcorestorage.NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("expected sqlite storage to create parent directory, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected db file to exist, got %v", err)
	}
}

func assertStorageError(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var storageErr *agentcore.StorageError
	if !errors.As(err, &storageErr) {
		t.Fatalf("expected storage error wrapper, got %T", err)
	}
	if !strings.Contains(err.Error(), "run") && !strings.Contains(err.Error(), "messages") && !strings.Contains(err.Error(), "validate") {
		t.Fatalf("expected contextual storage error, got %v", err)
	}
}
