package personal

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore/tools/native"
)

func TestUndoWithOptionsTargetsOneTurn(t *testing.T) {
	ctx := context.Background()
	repo := openObserveRepo(t)
	root := t.TempDir()
	sid := "undo-options"
	assert.NoErr(t, repo.CreateSession(ctx, store.Session{ID: sid, Cwd: root}))
	path := filepath.Join(root, "a.txt")
	assert.NoErr(t, os.WriteFile(path, []byte("zero"), 0o640))
	oldTurn := "turn-old"
	newTurn := "turn-new"
	assert.NoErr(t, repo.CreateTurn(ctx, store.Turn{ID: oldTurn, SessionID: sid, Status: store.TurnCompleted, StartedAt: time.Now().Add(-time.Hour)}))
	assert.NoErr(t, repo.CreateTurn(ctx, store.Turn{ID: newTurn, SessionID: sid, Status: store.TurnCompleted, StartedAt: time.Now()}))
	snapshots := NewSessionSnapshots(t.TempDir())
	record := func(turnID, callID, before, after string) {
		scope := withObserveScope(ctx, sid, turnID)
		_, err := snapshots.BeforeCall(scope, native.SnapshotRequest{CallID: callID, Tool: "file_write", Root: root, Rel: "a.txt", RealRel: "a.txt", Kind: native.SnapshotKindFile, Before: []byte(before)})
		assert.Require(t, assert.NoErr(t, err))
		assert.NoErr(t, os.WriteFile(path, []byte(after), 0o640))
		assert.NoErr(t, snapshots.AfterCall(scope, native.SnapshotResult{CallID: callID, Tool: "file_write", Root: root, Rel: "a.txt", RealRel: "a.txt", Kind: native.SnapshotKindFile, Status: native.SnapshotStatusComplete, AfterSHA256: nativeSHA256([]byte(after))}))
	}
	record(oldTurn, "old-call", "zero", "one")
	record(newTurn, "new-call", "one", "two")
	assert.NoErr(t, os.WriteFile(path, []byte("one"), 0o640))

	core := NewUndoCore(repo, snapshots, t.TempDir(), "")
	result, err := core.UndoWithOptions(ctx, "undo-optio", UndoOptions{TurnID: oldTurn})
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, sid, result.SessionID)
	assert.Eq(t, 1, result.Turns)
	assert.Eq(t, 1, result.Restored)
	content, err := os.ReadFile(path)
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, "zero", string(content))
	state, err := snapshots.LoadCheckpointState(sid, oldTurn)
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, SnapshotStateUndone, state.State)
	state, err = snapshots.LoadCheckpointState(sid, newTurn)
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, SnapshotStateOpen, state.State)
	session, err := repo.GetSession(ctx, sid)
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, true, session.Meta[undoNoticeQueueKey] != nil)
}
