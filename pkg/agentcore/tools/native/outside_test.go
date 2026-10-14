package native

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// outsideFixture builds a workspace, a read root and an added-directory set.
func outsideFixture(t *testing.T) (ws, root, ro, rw string, dirs *AddDirs) {
	t.Helper()
	ws, root, ro, rw = t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	dirs = NewAddDirs()
	dirs.Set([]AddDir{{Path: ro}, {Path: rw, Writable: true}})
	return ws, root, ro, rw, dirs
}

// resolved returns the symlink-resolved form of a path, which is what
// OutsideOf reports (on macOS /var is a link to /private/var, so a temp
// directory is compared in its real form).
func resolved(t *testing.T, path string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(path)
	assert.Require(t, assert.NoErr(t, err))
	return real
}

// TestOutsideOf: the directory a file tool call reaches outside the workspace,
// the read roots and the added directories is reported for the authorization
// question; everything already reachable is not.
func TestOutsideOf(t *testing.T) {
	ws, root, ro, rw, dirs := outsideFixture(t)
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "note.txt")
	assert.NoErr(t, os.WriteFile(outsideFile, []byte("x"), 0o644))
	roots := []string{root}

	t.Run("inside the workspace needs no authorization", func(t *testing.T) {
		_, ok := OutsideOf(ws, roots, dirs, "file_read", map[string]any{"path": filepath.Join(ws, "a.txt")})
		assert.False(t, ok)
	})
	t.Run("a relative path is left to the tool", func(t *testing.T) {
		_, ok := OutsideOf(ws, roots, dirs, "file_read", map[string]any{"path": "a.txt"})
		assert.False(t, ok)
	})
	t.Run("a configured read root needs no authorization", func(t *testing.T) {
		_, ok := OutsideOf(ws, roots, dirs, "file_read", map[string]any{"path": filepath.Join(root, "dep", "x.go")})
		assert.False(t, ok)
	})
	t.Run("an added directory needs no authorization for reading", func(t *testing.T) {
		_, ok := OutsideOf(ws, roots, dirs, "file_read", map[string]any{"path": filepath.Join(ro, "seed.txt")})
		assert.False(t, ok)
	})
	t.Run("an existing directory is offered itself", func(t *testing.T) {
		got, ok := OutsideOf(ws, roots, dirs, "file_list", map[string]any{"path": outsideDir})
		assert.True(t, ok)
		assert.Eq(t, resolved(t, outsideDir), got.Path)
		assert.Eq(t, outsideDir, got.Target)
		assert.False(t, got.Writable)
	})
	t.Run("a missing path is offered through its nearest existing ancestor", func(t *testing.T) {
		got, ok := OutsideOf(ws, roots, dirs, "file_read",
			map[string]any{"path": filepath.Join(outsideDir, "new", "deep", "a.txt")})
		assert.True(t, ok)
		assert.Eq(t, resolved(t, outsideDir), got.Path)
	})
	t.Run("a file is offered through its directory", func(t *testing.T) {
		got, ok := OutsideOf(ws, roots, dirs, "file_read", map[string]any{"path": outsideFile})
		assert.True(t, ok)
		assert.Eq(t, resolved(t, outsideDir), got.Path)
	})
	t.Run("file_read checks every path of a call", func(t *testing.T) {
		got, ok := OutsideOf(ws, roots, dirs, "file_read", map[string]any{
			"paths": []any{filepath.Join(ws, "ok.txt"), outsideFile}})
		assert.True(t, ok)
		assert.Eq(t, resolved(t, outsideDir), got.Path)
	})
	t.Run("a write into a read-only added directory is offered as writable", func(t *testing.T) {
		got, ok := OutsideOf(ws, roots, dirs, "file_write",
			map[string]any{"path": filepath.Join(ro, "new.txt"), "content": "x"})
		assert.True(t, ok)
		assert.Eq(t, resolved(t, ro), got.Path)
		assert.True(t, got.Writable)
	})
	t.Run("a write into a read-write added directory needs no authorization", func(t *testing.T) {
		_, ok := OutsideOf(ws, roots, dirs, "file_write",
			map[string]any{"path": filepath.Join(rw, "new.txt"), "content": "x"})
		assert.False(t, ok)
	})
	t.Run("the volume root is never offered", func(t *testing.T) {
		root := filepath.VolumeName(ws) + string(filepath.Separator)
		_, ok := OutsideOf(ws, roots, dirs, "file_list", map[string]any{"path": root})
		assert.False(t, ok)
	})
	t.Run("a tool that touches no file is ignored", func(t *testing.T) {
		_, ok := OutsideOf(ws, roots, dirs, "shell_exec", map[string]any{"command": "ls " + outsideDir})
		assert.False(t, ok)
	})
	t.Run("a relative path escaping the workspace is left to the tool", func(t *testing.T) {
		_, ok := OutsideOf(ws, roots, dirs, "file_read", map[string]any{"path": "../escape.txt"})
		assert.False(t, ok)
	})
}

// TestOutsideOfSymlink: the answer follows the link, so the person authorizes
// the directory the tool would really reach, not the one the path spells.
func TestOutsideOfSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs developer mode on Windows")
	}
	ws, root, _, _, dirs := outsideFixture(t)
	outsideDir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	assert.NoErr(t, os.Symlink(outsideDir, link))

	got, ok := OutsideOf(ws, []string{root}, dirs, "file_list", map[string]any{"path": link})
	assert.True(t, ok)
	assert.Eq(t, resolved(t, outsideDir), got.Path)
}
