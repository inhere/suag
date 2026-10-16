package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestDir(t *testing.T) {
	t.Run("a fresh, short directory outside the repository", func(t *testing.T) {
		dir := Dir(t)
		st, err := os.Stat(dir)
		assert.Require(t, assert.NoErr(t, err))
		assert.True(t, st.IsDir())
		assert.True(t, len(dir)+socketNameBytes <= socketPathLimit, "path is too long: %s", dir)
		// The file system temporary directory keeps it: go list ./... must never
		// scan a repository directory another test is deleting (issue suga-wab3).
		assert.Eq(t, os.TempDir(), filepath.Dir(dir))
		// A second call never hands out the first directory again.
		assert.NotEq(t, dir, Dir(t))
	})
}
