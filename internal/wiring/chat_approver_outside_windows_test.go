//go:build windows

package wiring

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"golang.org/x/sys/windows"
)

// shortPathName returns the 8.3 short name of path.
func shortPathName(t *testing.T, path string) string {
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

// shortSpelledPaths returns the respelling of the short-name test: every
// directory in its 8.3 short name, which is how a CI runner's %TEMP% reads
// ("C:\Users\RUNNER~1\AppData\Local\Temp"). The test is skipped when the volume
// does not generate short names (8.3 disabled there), because the shape it
// reproduces cannot occur then.
func shortSpelledPaths(t *testing.T) func(dir string) string {
	t.Helper()
	if probe := t.TempDir(); shortPathName(t, probe) == probe {
		t.Skip("8.3 short names are not generated on this volume")
	}
	return func(dir string) string { return shortPathName(t, dir) }
}

// TestChatApproverOutsideDirsShortNames is the CI runner's shape: the workspace
// and the outside directory are spelled with their 8.3 short names, while the
// authorization records the resolved real (long) directory. The granted read
// must reach the directory all the same; before the fix it was refused again
// with "path escape: ... is outside the workspace and the read-only dependency
// roots" (suga-o9md).
func TestChatApproverOutsideDirsShortNames(t *testing.T) {
	ctx := context.Background()
	asker := &scriptedAsker{answers: []string{"r"}}
	c := newOutsideCase(t, asker, outsideCaseOptions{paths: shortSpelledPaths(t)})
	// The two spellings really differ, or the case would reproduce nothing.
	assert.NotEq(t, c.outside, realDir(t, c.outside))
	arg := filepath.Join(c.outside, "b.txt")

	t.Run("a granted read reaches the short-spelled directory", func(t *testing.T) {
		res, err := c.read(ctx, arg)
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, res.Content, "outside")
		assert.Require(t, assert.Len(t, asker.questions, 1))
		assert.Eq(t, []string{realDir(t, c.outside) + ":ro"}, c.rt.AddDirs.List())

		// The grant covers the directory in either spelling: the next reads ask
		// nothing more, the short one and the long one alike.
		res, err = c.read(ctx, arg)
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, res.Content, "outside")
		res, err = c.read(ctx, filepath.Join(realDir(t, c.outside), "b.txt"))
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, res.Content, "outside")
		assert.Len(t, asker.questions, 1)
	})

	t.Run("a read inside the short-spelled workspace needs no question", func(t *testing.T) {
		res, err := c.read(ctx, filepath.Join(c.ws, "in.txt"))
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, res.Content, "inside")
		assert.Len(t, asker.questions, 1)
	})
}
