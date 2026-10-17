package personal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// TestContentConflict: the plan's conflict mark follows the rule undo applies -
// a target that is neither the written content nor (for a restore) already the
// snapshot is a conflict.
func TestContentConflict(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	item := CheckpointItem{Action: CheckpointActionRestore, Meta: SnapshotMeta{Root: root, Rel: "a.txt", RealRel: "a.txt",
		SHA256Before: nativeSHA256([]byte("old\n")), SHA256After: nativeSHA256([]byte("new\n"))}}
	write := func(text string) { assert.NoErr(t, os.WriteFile(path, []byte(text), 0o644)) }

	t.Run("unchanged since the write", func(t *testing.T) {
		write("new\n")
		assert.False(t, contentConflict(item))
	})
	t.Run("already the snapshot", func(t *testing.T) {
		write("old\n")
		assert.False(t, contentConflict(item))
	})
	t.Run("changed after the snapshot", func(t *testing.T) {
		write("manual\n")
		assert.True(t, contentConflict(item))
	})
}
