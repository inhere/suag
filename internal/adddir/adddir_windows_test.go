//go:build windows

package adddir

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"golang.org/x/sys/windows"
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
	t.Run("the same path twice in different spellings is one entry", func(t *testing.T) {
		short := windowsShortPath(t, dir)
		if short == dir {
			t.Skip("8.3 short names are not generated on this volume")
		}
		// A directory authorized at an outside-directory question is recorded in
		// its resolved (long) spelling while a typed :add-dir keeps what the
		// person wrote, so the two spellings really do meet.
		merged := Merge([]Dir{{Path: dir}}, []Dir{{Path: short, Writable: true}})
		assert.Require(t, assert.Eq(t, 1, len(merged)))
		assert.True(t, merged[0].Writable)
		assert.True(t, Has(merged, short))
		assert.Empty(t, Without(merged, short))
	})
}

// windowsShortPath returns the 8.3 short name of path.
func windowsShortPath(t *testing.T, path string) string {
	t.Helper()
	ptr, err := windows.UTF16PtrFromString(path)
	assert.Require(t, assert.NoErr(t, err))
	buf := make([]uint16, windows.MAX_PATH)
	for {
		n, err := windows.GetShortPathName(ptr, &buf[0], uint32(len(buf)))
		assert.Require(t, assert.NoErr(t, err))
		if int(n) < len(buf) {
			return windows.UTF16ToString(buf[:n])
		}
		buf = make([]uint16, n+1)
	}
}
