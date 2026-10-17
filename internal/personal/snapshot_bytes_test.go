package personal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestSnapshotDirectoryBytes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "snapshots")
	assert.NoErr(t, os.MkdirAll(dir, 0o700))
	assert.NoErr(t, os.WriteFile(filepath.Join(dir, "before"), []byte("abc"), 0o600))
	assert.NoErr(t, os.WriteFile(filepath.Join(dir, "meta.json"), []byte("{}"), 0o600))
	bytes, err := SnapshotDirectoryBytes(dir)
	assert.NoErr(t, err)
	assert.Eq(t, int64(5), bytes)

	missing, err := SnapshotDirectoryBytes(filepath.Join(root, "missing"))
	assert.NoErr(t, err)
	assert.Eq(t, int64(0), missing)
}
