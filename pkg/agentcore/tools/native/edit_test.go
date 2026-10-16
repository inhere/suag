package native

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/pkg/agentcore/api"
)

type editSnapshotter struct {
	before       []SnapshotRequest
	after        []SnapshotResult
	beforeResult SnapshotBefore
	beforeErr    error
	afterErr     error
	beforeHook   func(SnapshotRequest) error
	afterHook    func(SnapshotResult) error
	events       []string
}

func (s *editSnapshotter) BeforeCall(_ context.Context, req SnapshotRequest) (SnapshotBefore, error) {
	s.events = append(s.events, "before")
	s.before = append(s.before, req)
	if s.beforeHook != nil {
		if err := s.beforeHook(req); err != nil {
			return SnapshotBefore{}, err
		}
	}
	return s.beforeResult, s.beforeErr
}

func (s *editSnapshotter) AfterCall(_ context.Context, result SnapshotResult) error {
	s.events = append(s.events, "after")
	s.after = append(s.after, result)
	if s.afterHook != nil {
		if err := s.afterHook(result); err != nil {
			return err
		}
	}
	return s.afterErr
}

func TestEditFile(t *testing.T) {
	ws := t.TempDir()
	tool := &FileEditTool{workspace: ws}
	write := func(name, content string) {
		assert.NoErr(t, os.WriteFile(filepath.Join(ws, name), []byte(content), 0o644))
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(ws, name))
		assert.NoErr(t, err)
		return string(b)
	}
	edit := func(args map[string]any) error {
		_, err := tool.Execute(t.Context(), callWith(args))
		return err
	}

	t.Run("unique replacement", func(t *testing.T) {
		write("a.go", "a := 1\nb := 2\n")
		assert.NoErr(t, edit(map[string]any{"path": "a.go", "old_string": "b := 2", "new_string": "b := 3"}))
		assert.Eq(t, "a := 1\nb := 3\n", read("a.go"))
	})
	t.Run("ambiguous reports the count", func(t *testing.T) {
		write("b.go", "x\nx\nx\n")
		assert.ErrMsgContains(t, edit(map[string]any{"path": "b.go", "old_string": "x", "new_string": "y"}), "matches 3 places")
		assert.Eq(t, "x\nx\nx\n", read("b.go"))
	})
	t.Run("replace_all", func(t *testing.T) {
		write("c.go", "x\nx\n")
		assert.NoErr(t, edit(map[string]any{"path": "c.go", "old_string": "x", "new_string": "y", "replace_all": true}))
		assert.Eq(t, "y\ny\n", read("c.go"))
	})
	t.Run("not found", func(t *testing.T) {
		write("d.go", "hello\n")
		assert.ErrMsgContains(t, edit(map[string]any{"path": "d.go", "old_string": "bye", "new_string": "x"}), "not found")
	})
	t.Run("CRLF file keeps CRLF", func(t *testing.T) {
		write("e.txt", "one\r\ntwo\r\nthree\r\n")
		assert.NoErr(t, edit(map[string]any{"path": "e.txt", "old_string": "one\ntwo", "new_string": "1\n2\n2.5"}))
		assert.Eq(t, "1\r\n2\r\n2.5\r\nthree\r\n", read("e.txt"))
	})
	t.Run("identical strings and empty old_string are errors", func(t *testing.T) {
		write("f.go", "z\n")
		assert.ErrMsgContains(t, edit(map[string]any{"path": "f.go", "old_string": "z", "new_string": "z"}), "identical")
		assert.ErrMsgContains(t, edit(map[string]any{"path": "f.go", "old_string": "", "new_string": "z"}), "file_write")
	})
	t.Run("edits apply in order in one write", func(t *testing.T) {
		write("g.go", "a := 1\nb := 2\nc := 3\n")
		res, err := tool.Execute(t.Context(), callWith(map[string]any{"path": "g.go", "edits": []any{
			map[string]any{"old_string": "a := 1", "new_string": "a := 10"},
			map[string]any{"old_string": "a := 10\nb", "new_string": "a := 10\nbb"},
			map[string]any{"old_string": " 3", "new_string": " 30", "replace_all": true},
		}}))
		assert.Require(t, assert.NoError(t, err))
		assert.StrContains(t, res.Content, "3 replacement(s)")
		assert.Eq(t, "a := 10\nbb := 2\nc := 30\n", read("g.go"))
	})
	t.Run("a failing edit leaves the file unchanged", func(t *testing.T) {
		write("h.go", "a := 1\nb := 2\n")
		err := edit(map[string]any{"path": "h.go", "edits": []any{
			map[string]any{"old_string": "a := 1", "new_string": "a := 10"},
			map[string]any{"old_string": "nope", "new_string": "x"},
		}})
		assert.ErrMsgContains(t, err, "edits[1]")
		assert.ErrMsgContains(t, err, "no edit was applied")
		assert.Eq(t, "a := 1\nb := 2\n", read("h.go"))
	})
	t.Run("edits as a JSON string", func(t *testing.T) {
		write("i.go", "x\ny\n")
		assert.NoError(t, edit(map[string]any{"path": "i.go", "edits": "[{\"old_string\":\"x\",\"new_string\":\"X\"},{\"old_string\":\"y\",\"new_string\":\"Y\"}]"}))
		assert.Eq(t, "X\nY\n", read("i.go"))
	})
	t.Run("bad edits shapes are errors", func(t *testing.T) {
		write("j.go", "x\n")
		assert.ErrMsgContains(t, edit(map[string]any{"path": "j.go", "edits": []any{}}), "non-empty array")
		assert.ErrMsgContains(t, edit(map[string]any{"path": "j.go", "edits": []any{"x"}}), "edits[0] must be an object")
		assert.ErrMsgContains(t, edit(map[string]any{"path": "j.go", "old_string": "x", "new_string": "z",
			"edits": []any{map[string]any{"old_string": "x", "new_string": "z"}}}), "not both")
		assert.Eq(t, "x\n", read("j.go"))
	})
	t.Run("outside the workspace is rejected", func(t *testing.T) {
		assert.ErrMsgContains(t, edit(map[string]any{"path": "../x.go", "old_string": "a", "new_string": "b"}), "path escape")
	})
}

func TestFileEditSnapshotHook(t *testing.T) {
	t.Run("catalog injects the snapshotter", func(t *testing.T) {
		snapshots := &editSnapshotter{}
		tools := BuildTools(Env{Workspace: t.TempDir(), Snapshots: snapshots}, true)
		var edit *FileEditTool
		for _, tool := range tools {
			if candidate, ok := tool.(*FileEditTool); ok {
				edit = candidate
			}
		}
		assert.Require(t, assert.NotNil(t, edit))
		assert.Eq(t, snapshots, edit.snapshots)
	})

	t.Run("records before and after in order", func(t *testing.T) {
		workspace := t.TempDir()
		path := filepath.Join(workspace, "edit.txt")
		original := []byte{0x00, 0xff, 0x0a, 0x80}
		assert.NoError(t, os.WriteFile(path, original, 0o644))
		snapshots := &editSnapshotter{}
		tool := &FileEditTool{workspace: workspace, snapshots: snapshots}
		call := api.ToolCall{CallID: "edit-call", Arguments: map[string]any{
			"path": "edit.txt", "old_string": string(original), "new_string": "updated",
		}}
		_, err := tool.Execute(t.Context(), call)
		assert.Require(t, assert.NoError(t, err))
		assert.Eq(t, []string{"before", "after"}, snapshots.events)
		assert.Require(t, assert.Len(t, snapshots.before, 1))
		assert.Require(t, assert.Len(t, snapshots.after, 1))
		assert.Eq(t, original, snapshots.before[0].Before)
		assert.Eq(t, "edit-call", snapshots.before[0].CallID)
		assert.Eq(t, SnapshotKindFile, snapshots.before[0].Kind)
		assert.Eq(t, SnapshotStatusComplete, snapshots.after[0].Status)
		assert.Eq(t, sha256Hex([]byte("updated")), snapshots.after[0].AfterSHA256)
		assert.Eq(t, int64(len(original)), snapshots.after[0].Size)
		assert.Eq(t, "updated", string(mustReadFile(t, path)))
	})

	t.Run("unique match failure does not create a snapshot", func(t *testing.T) {
		workspace := t.TempDir()
		path := filepath.Join(workspace, "unchanged.txt")
		assert.NoError(t, os.WriteFile(path, []byte("same\nsame\n"), 0o644))
		snapshots := &editSnapshotter{}
		tool := &FileEditTool{workspace: workspace, snapshots: snapshots}
		_, err := tool.Execute(t.Context(), callWith(map[string]any{
			"path": "unchanged.txt", "old_string": "same", "new_string": "changed",
		}))
		assert.ErrMsgContains(t, err, "matches 2 places")
		assert.Empty(t, snapshots.before)
		assert.Empty(t, snapshots.after)
	})

	t.Run("reuses the original read instead of reading again", func(t *testing.T) {
		workspace := t.TempDir()
		path := filepath.Join(workspace, "once.txt")
		backup := filepath.Join(workspace, "once.before")
		original := []byte("before")
		assert.NoError(t, os.WriteFile(path, original, 0o644))
		snapshots := &editSnapshotter{beforeHook: func(SnapshotRequest) error {
			return os.Rename(path, backup)
		}}
		tool := &FileEditTool{workspace: workspace, snapshots: snapshots}
		_, err := tool.Execute(t.Context(), callWith(map[string]any{
			"path": "once.txt", "old_string": "before", "new_string": "after",
		}))
		assert.Require(t, assert.NoError(t, err))
		assert.Eq(t, "after", string(mustReadFile(t, path)))
		assert.Eq(t, original, mustReadFile(t, backup))
		assert.Eq(t, original, snapshots.before[0].Before)
	})

	t.Run("fails closed before writing when the snapshot fails", func(t *testing.T) {
		workspace := t.TempDir()
		path := filepath.Join(workspace, "keep.txt")
		assert.NoError(t, os.WriteFile(path, []byte("keep"), 0o644))
		snapshots := &editSnapshotter{beforeErr: errors.New("snapshot store unavailable")}
		tool := &FileEditTool{workspace: workspace, snapshots: snapshots}
		_, err := tool.Execute(t.Context(), callWith(map[string]any{
			"path": "keep.txt", "old_string": "keep", "new_string": "changed",
		}))
		assert.ErrMsgContains(t, err, "snapshot before")
		assert.Eq(t, "keep", string(mustReadFile(t, path)))
		assert.Empty(t, snapshots.after)
	})

	t.Run("records incomplete when the write fails after the snapshot", func(t *testing.T) {
		workspace := t.TempDir()
		parent := filepath.Join(workspace, "parent")
		path := filepath.Join(parent, "child.txt")
		assert.NoError(t, os.Mkdir(parent, 0o755))
		assert.NoError(t, os.WriteFile(path, []byte("before"), 0o644))
		snapshots := &editSnapshotter{beforeHook: func(SnapshotRequest) error {
			if err := os.Rename(parent, filepath.Join(workspace, "parent.backup")); err != nil {
				return err
			}
			return os.WriteFile(parent, []byte("blocking file"), 0o644)
		}}
		tool := &FileEditTool{workspace: workspace, snapshots: snapshots}
		_, err := tool.Execute(t.Context(), callWith(map[string]any{
			"path": "parent/child.txt", "old_string": "before", "new_string": "after",
		}))
		assert.Err(t, err)
		assert.Require(t, assert.Len(t, snapshots.after, 1))
		assert.Eq(t, SnapshotStatusIncomplete, snapshots.after[0].Status)
		assert.Empty(t, snapshots.after[0].AfterSHA256)
	})
}
