//go:build windows

package adddir

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// TestEntryWindowsDrive checks that the colon of a drive letter ("D:\docs") is
// not read as a mode suffix: only a trailing ":ro"/":rw" counts.
func TestEntryWindowsDrive(t *testing.T) {
	dir := t.TempDir()
	assert.True(t, strings.Contains(dir, ":"), dir)

	t.Run("a drive colon stays in the path", func(t *testing.T) {
		got, err := Entry(dir)
		assert.NoErr(t, err)
		assert.Eq(t, filepath.Clean(dir), got.Path)
		assert.False(t, got.Writable)
	})
	t.Run("the suffix after a drive path is the mode", func(t *testing.T) {
		got, err := Entry(strings.ToUpper(dir) + ":rw")
		assert.NoErr(t, err)
		assert.True(t, got.Writable)
		assert.True(t, strings.EqualFold(filepath.Clean(dir), got.Path), got.Path)
	})
	t.Run("the same path twice in different case is one entry", func(t *testing.T) {
		merged := Merge([]Dir{{Path: dir}}, []Dir{{Path: strings.ToUpper(dir), Writable: true}})
		assert.Eq(t, 1, len(merged))
		assert.True(t, merged[0].Writable)
	})
}
