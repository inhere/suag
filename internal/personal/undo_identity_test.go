package personal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore/tools/native"
)

func recordIdentityWrite(t *testing.T, f undoRestoreFixture, rel, realRel, before, after string) {
	t.Helper()
	scope := withObserveScope(f.ctx, "undo-restore", "turn-restore")
	_, err := f.snapshots.BeforeCall(scope, native.SnapshotRequest{
		CallID: "identity-" + rel, Tool: "file_write", Root: f.root, Rel: rel, RealRel: realRel,
		Kind: native.SnapshotKindFile, Before: []byte(before),
	})
	assert.Require(t, assert.NoErr(t, err))
	path := filepath.Join(f.root, filepath.FromSlash(rel))
	assert.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	assert.NoError(t, os.WriteFile(path, []byte(after), 0o640))
	assert.NoError(t, f.snapshots.AfterCall(scope, native.SnapshotResult{
		CallID: "identity-" + rel, Tool: "file_write", Root: f.root, Rel: rel, RealRel: realRel,
		Kind: native.SnapshotKindFile, Status: native.SnapshotStatusComplete,
		AfterSHA256: nativeSHA256([]byte(after)),
	}))
}

func TestUndoIdentityAndProtectedPathChecks(t *testing.T) {
	t.Run("changed final symlink is never followed even with force", func(t *testing.T) {
		f := newUndoRestoreFixture(t, "before")
		f.recordFileWrite(t, "symlink", "after")
		outside := filepath.Join(t.TempDir(), "outside.txt")
		assert.NoError(t, os.WriteFile(outside, []byte("outside"), 0o640))
		assert.NoError(t, os.Remove(f.path))
		if err := os.Symlink(outside, f.path); err != nil {
			t.Logf("symlink unavailable on this host: %v", err)
			return
		}

		result, err := f.core.Undo(f.ctx, "undo-restore", 1, true)
		assert.Require(t, assert.NoError(t, err))
		assert.Eq(t, 0, result.Restored)
		assert.Eq(t, 1, result.Skipped)
		content, err := os.ReadFile(outside)
		assert.Require(t, assert.NoError(t, err))
		assert.Eq(t, "outside", string(content))
	})

	t.Run("hard link is not used as a restore target", func(t *testing.T) {
		f := newUndoRestoreFixture(t, "before")
		f.recordFileWrite(t, "hardlink", "after")
		other := filepath.Join(f.root, "other.txt")
		assert.NoError(t, os.Remove(f.path))
		assert.NoError(t, os.WriteFile(other, []byte("after"), 0o640))
		if err := os.Link(other, f.path); err != nil {
			t.Logf("hard links unavailable on this host: %v", err)
			return
		}

		result, err := f.core.Undo(f.ctx, "undo-restore", 1, true)
		assert.Require(t, assert.NoError(t, err))
		assert.Eq(t, 0, result.Restored)
		assert.Eq(t, 1, result.Skipped)
		content, err := os.ReadFile(other)
		assert.Require(t, assert.NoError(t, err))
		assert.Eq(t, "after", string(content))
	})

	t.Run("directory at a file target is not overwritten", func(t *testing.T) {
		f := newUndoRestoreFixture(t, "before")
		f.recordFileWrite(t, "directory", "after")
		assert.NoError(t, os.Remove(f.path))
		assert.NoError(t, os.Mkdir(f.path, 0o755))

		result, err := f.core.Undo(f.ctx, "undo-restore", 1, true)
		assert.Require(t, assert.NoError(t, err))
		assert.Eq(t, 0, result.Restored)
		assert.Eq(t, 1, result.Skipped)
		info, err := os.Stat(f.path)
		assert.Require(t, assert.NoError(t, err))
		assert.True(t, info.IsDir())
	})

	t.Run("real relative path changes are rejected", func(t *testing.T) {
		f := newUndoRestoreFixture(t, "before")
		recordIdentityWrite(t, f, "a.txt", "other.txt", "before", "after")

		result, err := f.core.Undo(f.ctx, "undo-restore", 1, true)
		assert.Require(t, assert.NoError(t, err))
		assert.Eq(t, 0, result.Restored)
		assert.Eq(t, 1, result.Skipped)
		content, err := os.ReadFile(f.path)
		assert.Require(t, assert.NoError(t, err))
		assert.Eq(t, "after", string(content))
	})

	t.Run("protected git paths are never restored", func(t *testing.T) {
		f := newUndoRestoreFixture(t, "before")
		protected := filepath.Join(f.root, ".git", "config")
		assert.NoError(t, os.MkdirAll(filepath.Dir(protected), 0o755))
		assert.NoError(t, os.WriteFile(protected, []byte("before"), 0o640))
		rel := filepath.ToSlash(filepath.Join(".git", "config"))
		recordIdentityWrite(t, f, rel, rel, "before", "after")

		result, err := f.core.Undo(f.ctx, "undo-restore", 1, true)
		assert.Require(t, assert.NoError(t, err))
		assert.Eq(t, 0, result.Restored)
		assert.Eq(t, 1, result.Skipped)
		content, err := os.ReadFile(protected)
		assert.Require(t, assert.NoError(t, err))
		assert.Eq(t, "after", string(content))
	})

	t.Run("relative path escape cannot reach a sibling", func(t *testing.T) {
		f := newUndoRestoreFixture(t, "before")
		outside := filepath.Join(filepath.Dir(f.root), "outside.txt")
		assert.NoError(t, os.WriteFile(outside, []byte("after"), 0o640))
		recordIdentityWrite(t, f, "../outside.txt", "../outside.txt", "before", "after")

		result, err := f.core.Undo(f.ctx, "undo-restore", 1, true)
		assert.Require(t, assert.NoError(t, err))
		assert.Eq(t, 0, result.Restored)
		assert.Eq(t, 1, result.Skipped)
		content, err := os.ReadFile(outside)
		assert.Require(t, assert.NoError(t, err))
		assert.Eq(t, "after", string(content))
	})
}
