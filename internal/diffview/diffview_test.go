package diffview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// write puts a file into the test workspace.
func write(t *testing.T, dir, name, text string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCall: which call yields which change, and everything that yields none.
func TestCall(t *testing.T) {
	cwd := t.TempDir()
	write(t, cwd, "a.go", "a := 1\nb := 2\n")

	t.Run("file_write on an existing file keeps the old text", func(t *testing.T) {
		change, ok := Call(cwd, ToolWrite, map[string]any{"path": "a.go", "content": "x\n"})
		assert.Require(t, ok)
		assert.Eq(t, filepath.Join(cwd, "a.go"), change.Path)
		assert.Eq(t, "a.go", change.Rel)
		assert.Eq(t, ToolWrite, change.Tool)
		assert.Eq(t, "a := 1\nb := 2\n", *change.Old)
		assert.Eq(t, "x\n", *change.New)
		assert.False(t, change.NewFile())
		assert.Empty(t, change.RemovedLines)
	})

	t.Run("file_write creating a file has no old text", func(t *testing.T) {
		change, ok := Call(cwd, ToolWrite, map[string]any{"path": "sub/new.txt", "content": "one\ntwo\n"})
		assert.Require(t, ok)
		assert.Nil(t, change.Old)
		assert.True(t, change.NewFile())
		assert.Eq(t, "one\ntwo\n", *change.New)
	})

	t.Run("file_edit previews the replacements applied", func(t *testing.T) {
		change, ok := Call(cwd, ToolEdit, map[string]any{"path": "a.go", "old_string": "b := 2", "new_string": "b := 3"})
		assert.Require(t, ok)
		assert.Eq(t, "a := 1\nb := 2\n", *change.Old)
		assert.Eq(t, "a := 1\nb := 3\n", *change.New)
	})

	t.Run("file_edit with an edits list applies all of them", func(t *testing.T) {
		change, ok := Call(cwd, ToolEdit, map[string]any{"path": "a.go", "edits": []any{
			map[string]any{"old_string": "a := 1", "new_string": "a := 0"},
			map[string]any{"old_string": "b := 2", "new_string": "b := 3"},
		}})
		assert.Require(t, ok)
		assert.Eq(t, "a := 0\nb := 3\n", *change.New)
	})

	t.Run("file_edit that cannot apply has no change", func(t *testing.T) {
		for name, args := range map[string]map[string]any{
			"old_string missing from the file": {"path": "a.go", "old_string": "nope", "new_string": "x"},
			"file does not exist":              {"path": "new.go", "old_string": "a", "new_string": "b"},
		} {
			t.Run(name, func(t *testing.T) {
				change, ok := Call(cwd, ToolEdit, args)
				assert.False(t, ok)
				assert.Nil(t, change)
			})
		}
	})

	t.Run("file_remove counts the lines it takes away", func(t *testing.T) {
		change, ok := Call(cwd, ToolRemove, map[string]any{"path": "a.go"})
		assert.Require(t, ok)
		assert.Eq(t, 2, change.RemovedLines)
		assert.Eq(t, "a := 1\nb := 2\n", *change.Old)
		assert.Nil(t, change.New)
	})

	t.Run("file_remove of a missing path or a directory has no change", func(t *testing.T) {
		_, missing := Call(cwd, ToolRemove, map[string]any{"path": "gone.txt"})
		assert.False(t, missing)
		_, dir := Call(cwd, ToolRemove, map[string]any{"path": "sub"})
		assert.False(t, dir)
	})

	t.Run("other tools and paths have no change", func(t *testing.T) {
		for name, tc := range map[string]struct {
			tool string
			args map[string]any
		}{
			"another tool":         {tool: "file_read", args: map[string]any{"path": "a.go"}},
			"no path":              {tool: ToolWrite, args: map[string]any{"content": "x"}},
			"outside workspace":    {tool: ToolWrite, args: map[string]any{"path": filepath.Join(filepath.Dir(cwd), "x.txt"), "content": "x"}},
			"content not a string": {tool: ToolWrite, args: map[string]any{"path": "a.go", "content": 7}},
		} {
			t.Run(name, func(t *testing.T) {
				change, ok := Call(cwd, tc.tool, tc.args)
				assert.False(t, ok)
				assert.Nil(t, change)
			})
		}
	})

	t.Run("binary and oversized files have no change", func(t *testing.T) {
		write(t, cwd, "bin.dat", "a\x00b")
		write(t, cwd, "big.txt", strings.Repeat("x", MaxBytes+1))
		_, binary := Call(cwd, ToolWrite, map[string]any{"path": "bin.dat", "content": "x"})
		assert.False(t, binary)
		_, oversizedOld := Call(cwd, ToolWrite, map[string]any{"path": "big.txt", "content": "x"})
		assert.False(t, oversizedOld)
		_, oversizedNew := Call(cwd, ToolWrite, map[string]any{"path": "a.go", "content": strings.Repeat("x", MaxBytes+1)})
		assert.False(t, oversizedNew)
	})

	t.Run("a dangling symlink is not previewed", func(t *testing.T) {
		if err := os.Symlink("gone.txt", filepath.Join(cwd, "dangling.txt")); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}
		_, ok := Call(cwd, ToolWrite, map[string]any{"path": "dangling.txt", "content": "x"})
		assert.False(t, ok)
	})
}

// TestAtKeepsACPBehaviour: At resolves nothing itself, so a path outside the
// workspace (which the ACP server asks about before anything runs) has no change.
func TestAtKeepsACPBehaviour(t *testing.T) {
	cwd := t.TempDir()
	write(t, cwd, "a.txt", "old\n")
	change, ok := At(cwd, ToolWrite, filepath.Join(cwd, "a.txt"), map[string]any{"content": "new\n"})
	assert.Require(t, ok)
	assert.Eq(t, "old\n", *change.Old)
	assert.Eq(t, "new\n", *change.New)

	_, ok = At(cwd, ToolWrite, filepath.Join(filepath.Dir(cwd), "out.txt"), map[string]any{"content": "new\n"})
	assert.False(t, ok)
}

// TestRemovedLinesOfEmptyFile: an empty file is 0 lines and still a removal.
func TestRemovedLinesOfEmptyFile(t *testing.T) {
	cwd := t.TempDir()
	write(t, cwd, "empty.txt", "")
	change, ok := Call(cwd, ToolRemove, map[string]any{"path": "empty.txt"})
	assert.Require(t, ok)
	assert.Eq(t, 0, change.RemovedLines)
	assert.Eq(t, "", *change.Old)
}
