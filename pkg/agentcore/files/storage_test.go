package files_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/inhere/axon/pkg/agentcore"
	"github.com/inhere/axon/pkg/agentcore/files"
)

func TestStorageSaveRunCreatesExpectedLayout(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := files.NewStorage(root)
	state := &agentcore.RunState{
		RunID:    "run-layout",
		AgentID:  "agent-1",
		Status:   agentcore.RunRunning,
		Step:     2,
		Messages: []agentcore.Message{{Role: "user", Content: "snapshot"}},
	}

	if err := store.SaveRun(context.Background(), state); err != nil {
		t.Fatalf("expected save to succeed, got %v", err)
	}

	for _, rel := range []string{
		filepath.Join("run-layout", "state.json"),
		filepath.Join("run-layout", "meta.json"),
		filepath.Join("run-layout", "artifacts"),
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Fatalf("expected %s to exist, got %v", rel, err)
		}
	}
}

func TestStorageLoadRunReadsSnapshotState(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := files.NewStorage(root)
	saved := &agentcore.RunState{
		RunID:    "run-state",
		AgentID:  "agent-1",
		Status:   agentcore.RunCompleted,
		Step:     3,
		Messages: []agentcore.Message{{Role: "assistant", Content: "snapshot-only"}},
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
		t.Fatalf("expected loaded snapshot identifiers to match, got %#v", loaded)
	}
	if len(loaded.Messages) != 1 || loaded.Messages[0].Content != "snapshot-only" {
		t.Fatalf("expected snapshot messages to be loaded from state.json, got %#v", loaded.Messages)
	}
}

func TestStorageAppendAndLoadMessagesUseJSONL(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := files.NewStorage(root)
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
		t.Fatalf("expected appended messages in order, got %#v", got)
	}
	if got[1].Metadata["turn"] != float64(2) {
		t.Fatalf("expected metadata to round-trip through jsonl, got %#v", got[1].Metadata)
	}
}

func TestStorageLoadMessagesTreatsJSONLAsAuthorityForRecovery(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := files.NewStorage(root)
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
		t.Fatalf("expected load run to preserve snapshot payload, got %#v", loadedState.Messages)
	}
	if len(loadedMessages) != 2 || loadedMessages[0].Content != "from-log-1" || loadedMessages[1].Content != "from-log-2" {
		t.Fatalf("expected jsonl to remain the authoritative history, got %#v", loadedMessages)
	}

	recovered := *loadedState
	recovered.Messages = loadedMessages
	if len(recovered.Messages) != 2 || recovered.Messages[0].Content != "from-log-1" {
		t.Fatalf("expected recovery to rebuild messages from jsonl authority, got %#v", recovered.Messages)
	}
}

func TestStorageLoadMessagesReturnsEmptySliceForEmptyLog(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	runDir := filepath.Join(root, "run-empty")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("expected run dir creation to succeed, got %v", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "messages.jsonl"), nil, 0o644); err != nil {
		t.Fatalf("expected empty log creation to succeed, got %v", err)
	}

	store := files.NewStorage(root)
	got, err := store.LoadMessages(context.Background(), "run-empty")
	if err != nil {
		t.Fatalf("expected empty log to load, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no messages for empty log, got %#v", got)
	}
	if got == nil {
		t.Fatal("expected empty slice, got nil")
	}
}

func TestStorageRejectsEmptyRunID(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := files.NewStorage(root)

	_, err := store.LoadRun(context.Background(), "")
	assertStorageError(t, err)

	_, err = store.LoadMessages(context.Background(), "")
	assertStorageError(t, err)

	err = store.AppendMessages(context.Background(), "", agentcore.Message{Role: "user", Content: "hi"})
	assertStorageError(t, err)

	err = store.SaveRun(context.Background(), &agentcore.RunState{})
	assertStorageError(t, err)
}

func TestStorageMissingRunReturnsWrappedNotExist(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := files.NewStorage(root)

	_, err := store.LoadRun(context.Background(), "missing-run")
	assertStorageError(t, err)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected missing run state to wrap os.ErrNotExist, got %v", err)
	}

	_, err = store.LoadMessages(context.Background(), "missing-run")
	assertStorageError(t, err)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected missing message log to wrap os.ErrNotExist, got %v", err)
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
}
