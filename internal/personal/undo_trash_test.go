package personal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore/tools/native"
)

type undoTrashFixture struct {
	ctx       context.Context
	repo      *store.Store
	snapshots *SessionSnapshots
	core      *UndoCore
	root      string
	sid       string
	turnID    string
}

func newUndoTrashFixture(t *testing.T) undoTrashFixture {
	t.Helper()
	ctx := context.Background()
	repo := openObserveRepo(t)
	root := t.TempDir()
	sid, turnID := "undo-trash", "turn-trash"
	assert.NoErr(t, repo.CreateSession(ctx, store.Session{ID: sid, Cwd: root}))
	assert.NoErr(t, repo.CreateTurn(ctx, store.Turn{ID: turnID, SessionID: sid, Status: store.TurnCompleted, StartedAt: time.Now().UTC()}))
	snapshots := NewSessionSnapshots(t.TempDir())
	return undoTrashFixture{ctx: ctx, repo: repo, snapshots: snapshots,
		core: NewUndoCore(repo, snapshots, t.TempDir(), ""), root: root, sid: sid, turnID: turnID}
}

func (f undoTrashFixture) recordTrash(t *testing.T, callID, rel, entry string, purged []string) {
	t.Helper()
	scope := withObserveScope(f.ctx, f.sid, f.turnID)
	_, err := f.snapshots.BeforeCall(scope, native.SnapshotRequest{
		CallID: callID, Tool: "file_remove", Root: f.root, Rel: rel, RealRel: rel, Kind: native.SnapshotKindTrash,
	})
	assert.Require(t, assert.NoErr(t, err))
	assert.NoErr(t, f.snapshots.AfterCall(scope, native.SnapshotResult{
		CallID: callID, Tool: "file_remove", Root: f.root, Rel: rel, RealRel: rel,
		Kind: native.SnapshotKindTrash, Size: 1, Status: native.SnapshotStatusComplete,
		TrashEntries: []string{entry}, PurgedDirs: purged,
	}))
}

func writeTrashOrigin(t *testing.T, entry, original string) {
	t.Helper()
	assert.NoErr(t, os.MkdirAll(entry, 0o700))
	data, err := json.Marshal(map[string]string{"path": original})
	assert.Require(t, assert.NoErr(t, err))
	assert.NoErr(t, os.WriteFile(filepath.Join(entry, "origin.json"), data, 0o600))
}

func TestUndoTrashRecovery(t *testing.T) {
	t.Run("restores a file after validating origin", func(t *testing.T) {
		f := newUndoTrashFixture(t)
		path := filepath.Join(f.root, "gone.txt")
		entry := filepath.Join(t.TempDir(), "trash-entry")
		writeTrashOrigin(t, entry, path)
		assert.NoErr(t, os.WriteFile(filepath.Join(entry, "gone.txt"), []byte("before"), 0o640))
		f.recordTrash(t, "remove-file", "gone.txt", entry, nil)

		result, err := f.core.Undo(f.ctx, f.sid, 1, false)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, 1, result.Restored)
		content, err := os.ReadFile(path)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "before", string(content))
		state, err := f.snapshots.LoadCheckpointState(f.sid, f.turnID)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, SnapshotStateUndone, state.State)
	})

	t.Run("origin mismatch and existing destination are conflicts", func(t *testing.T) {
		f := newUndoTrashFixture(t)
		path := filepath.Join(f.root, "gone.txt")
		entry := filepath.Join(t.TempDir(), "bad-origin")
		writeTrashOrigin(t, entry, filepath.Join(f.root, "other.txt"))
		assert.NoErr(t, os.WriteFile(filepath.Join(entry, "gone.txt"), []byte("before"), 0o640))
		f.recordTrash(t, "bad-origin", "gone.txt", entry, nil)
		result, err := f.core.Undo(f.ctx, f.sid, 1, false)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, 0, result.Restored)
		assert.Eq(t, 1, result.Skipped)
		_, err = os.Stat(path)
		assert.True(t, os.IsNotExist(err))

		f = newUndoTrashFixture(t)
		path = filepath.Join(f.root, "exists.txt")
		entry = filepath.Join(t.TempDir(), "existing")
		writeTrashOrigin(t, entry, path)
		assert.NoErr(t, os.WriteFile(filepath.Join(entry, "exists.txt"), []byte("trash"), 0o640))
		assert.NoErr(t, os.WriteFile(path, []byte("manual"), 0o640))
		f.recordTrash(t, "existing", "exists.txt", entry, nil)
		result, err = f.core.Undo(f.ctx, f.sid, 1, false)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, 0, result.Restored)
		assert.Eq(t, 1, result.Skipped)
		content, err := os.ReadFile(path)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "manual", string(content))
	})

	t.Run("expired trash is unrecoverable and mixed deletion is partial", func(t *testing.T) {
		f := newUndoTrashFixture(t)
		missing := filepath.Join(t.TempDir(), "expired")
		f.recordTrash(t, "expired", "expired.txt", missing, nil)
		result, err := f.core.Undo(f.ctx, f.sid, 1, false)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, 1, result.Unrecoverable)
		state, err := f.snapshots.LoadCheckpointState(f.sid, f.turnID)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, SnapshotStatePartial, state.State)

		f = newUndoTrashFixture(t)
		path := filepath.Join(f.root, "mixed")
		entry := filepath.Join(t.TempDir(), "mixed")
		writeTrashOrigin(t, entry, path)
		assert.NoErr(t, os.MkdirAll(filepath.Join(entry, "mixed"), 0o700))
		assert.NoErr(t, os.WriteFile(filepath.Join(entry, "mixed", "kept.txt"), []byte("kept"), 0o640))
		f.recordTrash(t, "mixed", "mixed", entry, []string{"mixed/tmp"})
		result, err = f.core.Undo(f.ctx, f.sid, 1, false)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, 1, result.Restored)
		state, err = f.snapshots.LoadCheckpointState(f.sid, f.turnID)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, SnapshotStatePartial, state.State)
		_, err = os.Stat(filepath.Join(path, "kept.txt"))
		assert.NoError(t, err)
	})

	t.Run("Windows case variants do not overwrite one target", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Log("Windows-only case-folding behavior")
			return
		}
		f := newUndoTrashFixture(t)
		path := filepath.Join(f.root, "Case.txt")
		entry := filepath.Join(t.TempDir(), "case")
		writeTrashOrigin(t, entry, path)
		assert.NoErr(t, os.WriteFile(filepath.Join(entry, "Case.txt"), []byte("before"), 0o640))
		assert.NoErr(t, os.WriteFile(filepath.Join(f.root, "case.txt"), []byte("manual"), 0o640))
		f.recordTrash(t, "case", "Case.txt", entry, nil)
		result, err := f.core.Undo(f.ctx, f.sid, 1, false)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, 0, result.Restored)
		assert.Eq(t, 1, result.Skipped)
	})
}
