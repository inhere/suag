package personal

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore/tools/native"
)

func TestCheckpointSelection(t *testing.T) {
	ctx := context.Background()
	repo := openObserveRepo(t)
	assert.NoErr(t, repo.CreateSession(ctx, store.Session{ID: "session-alpha", Cwd: t.TempDir()}))
	snapshots := NewSessionSnapshots(t.TempDir())
	base := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)
	createTurn := func(t *testing.T, id string, at time.Time) {
		t.Helper()
		assert.NoErr(t, repo.CreateTurn(ctx, store.Turn{ID: id, SessionID: "session-alpha", Status: store.TurnCompleted, StartedAt: at}))
	}
	createSnapshot := func(t *testing.T, turnID, callID, kind string) {
		t.Helper()
		scope := withObserveScope(ctx, "session-alpha", turnID)
		before := []byte("before-" + callID)
		result, err := snapshots.BeforeCall(scope, native.SnapshotRequest{CallID: callID, Tool: "file_write", Root: t.TempDir(), Rel: callID, RealRel: callID, Kind: kind, Before: before})
		assert.Require(t, assert.NoErr(t, err))
		assert.Empty(t, result.Unrecoverable)
		assert.NoErr(t, snapshots.AfterCall(scope, native.SnapshotResult{CallID: callID, Status: native.SnapshotStatusComplete, Size: int64(len(before))}))
	}

	t.Run("turn order, session prefix and action counts", func(t *testing.T) {
		createTurn(t, "turn-old", base)
		createSnapshot(t, "turn-old", "old", native.SnapshotKindFile)
		createTurn(t, "turn-new", base.Add(time.Hour))
		createSnapshot(t, "turn-new", "new-file", native.SnapshotKindFile)
		createSnapshot(t, "turn-new", "new-absent", native.SnapshotKindAbsent)
		rows, err := ListCheckpoints(ctx, repo, snapshots, "session-al", 0)
		assert.Require(t, assert.NoErr(t, err))
		assert.Require(t, assert.Len(t, rows, 2))
		assert.Eq(t, "turn-new", rows[0].TurnID)
		assert.Eq(t, "turn-old", rows[1].TurnID)
		assert.Eq(t, 2, rows[0].Calls)
		assert.Eq(t, 1, rows[0].Restorable)
		assert.Eq(t, 1, rows[0].Deletable)
		assert.Eq(t, int64(len("before-new-file"))+int64(len("before-new-absent")), rows[0].Bytes)
	})

	t.Run("undone is excluded while partial remains explicit", func(t *testing.T) {
		createTurn(t, "turn-undone", base.Add(2*time.Hour))
		createSnapshot(t, "turn-undone", "undone", native.SnapshotKindFile)
		assert.NoErr(t, snapshots.SetCheckpointState("session-alpha", "turn-undone", SnapshotStateUndone))
		createTurn(t, "turn-partial", base.Add(3*time.Hour))
		createSnapshot(t, "turn-partial", "partial", native.SnapshotKindFile)
		assert.NoErr(t, snapshots.SetCheckpointState("session-alpha", "turn-partial", SnapshotStatePartial))

		rows, err := ListCheckpoints(ctx, repo, snapshots, "session-alpha", 0)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, SnapshotStatePartial, rows[0].State)
		assert.Eq(t, SnapshotStateUndone, rows[1].State)
		selected, err := SelectUndoCheckpoints(ctx, repo, snapshots, "session-alpha", 1)
		assert.Require(t, assert.NoErr(t, err))
		assert.Require(t, assert.Len(t, selected, 1))
		assert.Eq(t, "turn-partial", selected[0].TurnID)
		assert.Eq(t, SnapshotStatePartial, selected[0].State)
	})
}

func TestCheckpointShellSources(t *testing.T) {
	ctx := context.Background()
	repo := openObserveRepo(t)
	assert.NoErr(t, repo.CreateSession(ctx, store.Session{ID: "shell-session", Cwd: t.TempDir()}))
	snapshots := NewSessionSnapshots(t.TempDir())
	base := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	assert.NoErr(t, repo.CreateTurn(ctx, store.Turn{ID: "shell-only", SessionID: "shell-session", Status: store.TurnCompleted, StartedAt: base.Add(time.Minute)}))
	for i := 0; i < 6; i++ {
		_, err := repo.AppendEvent(ctx, store.Event{SessionID: "shell-session", TurnID: "shell-only", Kind: store.EventTool, Name: "shell_exec", Status: "ok", StartedAt: base.Add(time.Minute).Add(time.Duration(i) * time.Second), Attrs: map[string]any{
			"arg": "command=echo token=top-secret-" + string(rune('a'+i)), "task_id": "task-1",
		}})
		assert.NoErr(t, err)
	}
	assert.NoErr(t, repo.CreateTurn(ctx, store.Turn{ID: "file-turn", SessionID: "shell-session", Status: store.TurnCompleted, StartedAt: base}))
	scope := withObserveScope(ctx, "shell-session", "file-turn")
	_, err := snapshots.BeforeCall(scope, native.SnapshotRequest{CallID: "file", Tool: "file_write", Root: t.TempDir(), Rel: "a.txt", RealRel: "a.txt", Kind: native.SnapshotKindAbsent})
	assert.Require(t, assert.NoErr(t, err))
	assert.NoErr(t, snapshots.AfterCall(scope, native.SnapshotResult{CallID: "file", Status: native.SnapshotStatusComplete}))

	selected, err := SelectUndoCheckpoints(ctx, repo, snapshots, "shell-s", 1)
	assert.Require(t, assert.NoErr(t, err))
	assert.Require(t, assert.Len(t, selected, 2))
	assert.True(t, selected[0].IsShellOnly())
	assert.Eq(t, "shell-only", selected[0].TurnID)
	assert.Eq(t, "file-turn", selected[1].TurnID)
	assert.Require(t, assert.Len(t, selected[0].Shell, 5))
	assert.Eq(t, "task-1", selected[0].Shell[0].TaskID)
	assert.NotContains(t, selected[0].Shell[0].Command, "top-secret")
	assert.Contains(t, selected[0].Shell[0].Command, "[REDACTED]")
	assert.True(t, strings.Contains(selected[0].Shell[0].Command, "command=echo"))
}

func TestCheckpointItemClassification(t *testing.T) {
	ctx := context.Background()
	repo := openObserveRepo(t)
	assert.NoErr(t, repo.CreateSession(ctx, store.Session{ID: "classify", Cwd: t.TempDir()}))
	assert.NoErr(t, repo.CreateTurn(ctx, store.Turn{ID: "turn-1", SessionID: "classify", Status: store.TurnCompleted}))
	snapshots := NewSessionSnapshots(t.TempDir())
	scope := withObserveScope(ctx, "classify", "turn-1")
	for i, kind := range []string{native.SnapshotKindAbsent, native.SnapshotKindPurged} {
		callID := string(rune('a' + i))
		_, err := snapshots.BeforeCall(scope, native.SnapshotRequest{CallID: callID, Tool: "file_remove", Root: t.TempDir(), Rel: callID, RealRel: callID, Kind: kind})
		assert.Require(t, assert.NoErr(t, err))
		assert.NoErr(t, snapshots.AfterCall(scope, native.SnapshotResult{CallID: callID, Status: native.SnapshotStatusComplete}))
	}
	rows, err := ListCheckpoints(ctx, repo, snapshots, "classify", 0)
	assert.Require(t, assert.NoErr(t, err))
	assert.Require(t, assert.Len(t, rows, 1))
	assert.Eq(t, CheckpointActionDelete, rows[0].Items[0].Action)
	assert.Eq(t, "本回合新建的文件", rows[0].Items[0].Reason)
	assert.Eq(t, CheckpointActionUnrecoverable, rows[0].Items[1].Action)
	assert.Eq(t, 1, rows[0].Unrecoverable)
}
