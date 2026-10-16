package native

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestMovePathDirectoryAcrossVolumes(t *testing.T) {
	previous := renamePath
	renamePath = func(string, string) error { return errors.New("invalid cross-device link") }
	t.Cleanup(func() { renamePath = previous })

	src := filepath.Join(t.TempDir(), "from")
	dst := filepath.Join(t.TempDir(), "to")
	assert.NoError(t, os.MkdirAll(filepath.Join(src, "nested"), 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(src, "nested", "file.txt"), []byte("content"), 0o640))
	assert.NoError(t, MovePath(src, dst))
	content, err := os.ReadFile(filepath.Join(dst, "nested", "file.txt"))
	assert.Require(t, assert.NoError(t, err))
	assert.Eq(t, "content", string(content))
	_, err = os.Stat(src)
	assert.True(t, os.IsNotExist(err))
}
