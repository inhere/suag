package native

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/pkg/agentcore/api"
)

// addDirFixture builds a workspace plus one added read-only and one added
// read-write directory and the tool table of a writable session. The tools are
// built before Set, as the runtime is: they must read the set at call time.
func addDirFixture(t *testing.T) (ro, rw string, dirs *AddDirs, byName map[string]api.Tool) {
	t.Helper()
	ws := t.TempDir()
	ro, rw = t.TempDir(), t.TempDir()
	assert.NoErr(t, os.WriteFile(filepath.Join(ws, "a.txt"), []byte("alpha\n"), 0o644))
	assert.NoErr(t, os.WriteFile(filepath.Join(ro, "ro.txt"), []byte("read only\n"), 0o644))
	assert.NoErr(t, os.WriteFile(filepath.Join(rw, "rw.txt"), []byte("read write\n"), 0o644))
	dirs = NewAddDirs()
	env := Env{Workspace: ws, TrashDir: t.TempDir(), AddDirs: dirs}
	byName = map[string]api.Tool{}
	for _, tool := range BuildTools(env, true) {
		byName[tool.Name()] = tool
	}
	dirs.Set([]AddDir{{Path: ro}, {Path: rw, Writable: true}})
	return ro, rw, dirs, byName
}

// execErr runs a tool call and returns its error (nil when it succeeded).
func execErr(t *testing.T, tool api.Tool, args map[string]any) error {
	t.Helper()
	_, err := tool.Execute(context.Background(), api.ToolCall{CallID: "c", Arguments: args})
	return err
}

func TestAddDirsAccess(t *testing.T) {
	ro, rw, dirs, byName := addDirFixture(t)

	t.Run("reads an added directory whatever its mode", func(t *testing.T) {
		assert.StrContains(t, execTool(t, byName["file_read"], map[string]any{"path": filepath.Join(ro, "ro.txt")}), "read only")
		assert.StrContains(t, execTool(t, byName["file_read"], map[string]any{"path": filepath.Join(rw, "rw.txt")}), "read write")
		assert.StrContains(t, execTool(t, byName["file_list"], map[string]any{"dir": ro}), "ro.txt")
		assert.StrContains(t, execTool(t, byName["file_grep"], map[string]any{"pattern": "read only", "path": ro}), "ro.txt")
		assert.StrContains(t, execTool(t, byName["file_find"], map[string]any{"pattern": "*.txt", "path": rw}), "rw.txt")
	})

	t.Run("writes the read-write directory", func(t *testing.T) {
		assert.StrContains(t, execTool(t, byName["file_write"],
			map[string]any{"path": filepath.Join(rw, "new.txt"), "content": "made\n"}), "wrote")
		data, err := os.ReadFile(filepath.Join(rw, "new.txt"))
		assert.NoErr(t, err)
		assert.Eq(t, "made\n", string(data))

		assert.StrContains(t, execTool(t, byName["file_edit"],
			map[string]any{"path": filepath.Join(rw, "rw.txt"), "old_string": "write\n", "new_string": "written\n"}), "edited")
		edited, err := os.ReadFile(filepath.Join(rw, "rw.txt"))
		assert.NoErr(t, err)
		assert.Eq(t, "read written\n", string(edited))

		assert.StrContains(t, execTool(t, byName["file_remove"],
			map[string]any{"path": filepath.Join(rw, "new.txt")}), "removed")
		_, err = os.Stat(filepath.Join(rw, "new.txt"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("refuses writing a read-only added directory", func(t *testing.T) {
		for _, call := range []struct {
			tool string
			args map[string]any
		}{
			{"file_write", map[string]any{"path": filepath.Join(ro, "x.txt"), "content": "x"}},
			{"file_edit", map[string]any{"path": filepath.Join(ro, "ro.txt"), "old_string": "read", "new_string": "READ"}},
			{"file_remove", map[string]any{"path": filepath.Join(ro, "ro.txt")}},
		} {
			assert.ErrMsgContains(t, execErr(t, byName[call.tool], call.args),
				"outside the workspace and the added read-write directories", call.tool)
		}
		data, err := os.ReadFile(filepath.Join(ro, "ro.txt"))
		assert.NoErr(t, err)
		assert.Eq(t, "read only\n", string(data))
	})

	t.Run("refuses a directory that was not added", func(t *testing.T) {
		other := filepath.Join(t.TempDir(), "other.txt")
		assert.NoErr(t, os.WriteFile(other, []byte("x\n"), 0o644))
		for _, name := range []string{"file_read", "file_write", "file_edit", "file_remove"} {
			args := map[string]any{"path": other}
			if name == "file_write" {
				args["content"] = "x"
			}
			if name == "file_edit" {
				args["old_string"], args["new_string"] = "x", "y"
			}
			assert.ErrMsgContains(t, execErr(t, byName[name], args), "outside the workspace", name)
		}
	})

	t.Run("the set is read at call time", func(t *testing.T) {
		// Reverse check of the late binding: emptying the set (as a session
		// without --add-dir does) must refuse the directory again.
		dirs.Set(nil)
		assert.ErrMsgContains(t, execErr(t, byName["file_read"], map[string]any{"path": filepath.Join(ro, "ro.txt")}),
			"outside workspace")
		assert.ErrMsgContains(t, execErr(t, byName["file_write"],
			map[string]any{"path": filepath.Join(rw, "new.txt"), "content": "x"}), "outside workspace")
		dirs.Set([]AddDir{{Path: ro}, {Path: rw, Writable: true}})
	})

	t.Run("a path takes the mode it was set with", func(t *testing.T) {
		// Only an explicit :rw entry grants writing: the same directory as :ro
		// is read-only (check the mode the front end resolved).
		dirs.Set([]AddDir{{Path: ro}})
		// No :rw entry at all: the write tools do not even try the directory.
		assert.ErrMsgContains(t, execErr(t, byName["file_write"],
			map[string]any{"path": filepath.Join(ro, "x.txt"), "content": "x"}),
			"outside workspace")
		dirs.Set([]AddDir{{Path: ro, Writable: true}})
		assert.StrContains(t, execTool(t, byName["file_write"],
			map[string]any{"path": filepath.Join(ro, "x.txt"), "content": "x"}), "wrote")
		dirs.Set([]AddDir{{Path: ro}, {Path: rw, Writable: true}})
	})

	t.Run("refuses removing the root of an added directory", func(t *testing.T) {
		assert.ErrMsgContains(t, execErr(t, byName["file_remove"], map[string]any{"path": rw, "recursive": true}),
			"refusing to remove the root of an added directory")
	})
}

func TestAddDirsSet(t *testing.T) {
	t.Run("a nil set is empty everywhere", func(t *testing.T) {
		var d *AddDirs
		d.Set([]AddDir{{Path: t.TempDir()}}) // must not panic
		assert.Empty(t, d.Snapshot())
		assert.Empty(t, d.ReadPaths())
		assert.Empty(t, d.WritePaths())
		assert.Empty(t, d.List())
	})
	t.Run("paths are cleaned and the first entry of a path wins", func(t *testing.T) {
		dir := t.TempDir()
		d := NewAddDirs()
		d.Set([]AddDir{{Path: filepath.Join(dir, "sub", "..")}, {Path: dir, Writable: true}, {Path: ""}})
		assert.Eq(t, []AddDir{{Path: filepath.Clean(dir)}}, d.Snapshot())
	})
	t.Run("ReadPaths keeps every entry, WritePaths only :rw", func(t *testing.T) {
		ro, rw := t.TempDir(), t.TempDir()
		d := NewAddDirs()
		d.Set([]AddDir{{Path: ro}, {Path: rw, Writable: true}})
		assert.Eq(t, []string{ro, rw}, d.ReadPaths())
		assert.Eq(t, []string{rw}, d.WritePaths())
		assert.Eq(t, []string{ro + ":ro", rw + ":rw"}, d.List())
	})
}

func TestResolveWrite(t *testing.T) {
	ws, dir := t.TempDir(), t.TempDir()
	file := filepath.Join(dir, "f.txt")
	assert.NoErr(t, os.WriteFile(file, []byte("x\n"), 0o644))

	t.Run("workspace paths stay in the workspace", func(t *testing.T) {
		target, err := resolveWrite(ws, []string{dir}, "a/b.txt")
		assert.NoErr(t, err)
		assert.Eq(t, ws, target.base)
		assert.False(t, target.outside)
	})
	t.Run("an absolute path under a writable directory", func(t *testing.T) {
		target, err := resolveWrite(ws, []string{dir}, file)
		assert.NoErr(t, err)
		assert.Eq(t, dir, target.base)
		assert.Eq(t, file, target.abs)
		assert.True(t, target.outside)
	})
	t.Run("a relative escape is refused even with writable directories", func(t *testing.T) {
		rel, err := filepath.Rel(ws, file)
		assert.NoErr(t, err)
		_, err = resolveWrite(ws, []string{dir}, rel)
		assert.ErrMsgContains(t, err, "outside workspace")
	})
	t.Run("outside both is refused", func(t *testing.T) {
		_, err := resolveWrite(ws, []string{dir}, filepath.Join(filepath.Dir(dir), "elsewhere.txt"))
		assert.ErrMsgContains(t, err, "outside the workspace and the added read-write directories")
	})
}
