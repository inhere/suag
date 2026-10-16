package native

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestMovePathAcrossVolumes(t *testing.T) {
	previous := renamePath
	renamePath = func(string, string) error { return errors.New("invalid cross-device link") }
	t.Cleanup(func() { renamePath = previous })

	src := filepath.Join(t.TempDir(), "from.txt")
	dst := filepath.Join(t.TempDir(), "to.txt")
	assert.NoErr(t, os.WriteFile(src, []byte("content"), 0o640))
	assert.NoErr(t, MovePath(src, dst))
	content, err := os.ReadFile(dst)
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, "content", string(content))
	_, err = os.Stat(src)
	assert.True(t, os.IsNotExist(err))
}
