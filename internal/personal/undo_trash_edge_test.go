package personal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore/tools/native"
)

func TestUndoTrashReservedNameAndExpiry(t *testing.T) {
	t.Run("restores a removed origin.json payload", func(t *testing.T) {
		f := newUndoTrashFixture(t)
		path := filepath.Join(f.root, "origin.json")
		entry := filepath.Join(t.TempDir(), "origin-name")
		writeTrashOrigin(t, entry, path)
		assert.NoError(t, os.WriteFile(filepath.Join(entry, ".suga-content"), []byte("original bytes"), 0o640))
		origin, err := json.Marshal(map[string]string{
			"path": path, "name": ".suga-content", "removed_at": time.Now().UTC().Format(time.RFC3339),
		})
		assert.Require(t, assert.NoError(t, err))
		assert.NoError(t, os.WriteFile(filepath.Join(entry, "origin.json"), origin, 0o600))
		f.recordTrash(t, "origin-name", "origin.json", entry, nil)

		result, err := f.core.Undo(f.ctx, f.sid, 1, false)
		assert.Require(t, assert.NoError(t, err))
		assert.Eq(t, 1, result.Restored)
		content, err := os.ReadFile(path)
		assert.Require(t, assert.NoError(t, err))
		assert.Eq(t, "original bytes", string(content))
	})

	t.Run("an existing but expired entry is unrecoverable", func(t *testing.T) {
		f := newUndoTrashFixture(t)
		path := filepath.Join(f.root, "old.txt")
		entry := filepath.Join(t.TempDir(), "expired-name")
		writeTrashOrigin(t, entry, path)
		origin, err := json.Marshal(map[string]string{
			"path": path, "name": "old.txt",
			"removed_at": time.Now().UTC().Add(-native.TrashRetention - time.Hour).Format(time.RFC3339),
		})
		assert.Require(t, assert.NoError(t, err))
		assert.NoError(t, os.WriteFile(filepath.Join(entry, "origin.json"), origin, 0o600))
		assert.NoError(t, os.WriteFile(filepath.Join(entry, "old.txt"), []byte("old"), 0o640))
		f.recordTrash(t, "expired-name", "old.txt", entry, nil)

		result, err := f.core.Undo(f.ctx, f.sid, 1, false)
		assert.Require(t, assert.NoError(t, err))
		assert.Eq(t, 1, result.Unrecoverable)
		_, err = os.Stat(path)
		assert.True(t, os.IsNotExist(err))
	})
}
