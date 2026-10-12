package native

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

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
		assert.Require(t, assert.NoErr(t, err))
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
		assert.NoErr(t, edit(map[string]any{"path": "i.go", "edits": `[{"old_string":"x","new_string":"X"},{"old_string":"y","new_string":"Y"}]`}))
		assert.Eq(t, "X\nY\n", read("i.go"))
	})
	t.Run("bad edits shapes are errors", func(t *testing.T) {
		write("j.go", "x\n")
		assert.ErrMsgContains(t, edit(map[string]any{"path": "j.go", "edits": []any{}}), "non-empty array")
		assert.ErrMsgContains(t, edit(map[string]any{"path": "j.go", "edits": []any{"x"}}), "edits[0] must be an object")
		assert.ErrMsgContains(t, edit(map[string]any{"path": "j.go", "old_string": "x", "new_string": "y",
			"edits": []any{map[string]any{"old_string": "x", "new_string": "z"}}}), "not both")
		assert.Eq(t, "x\n", read("j.go"))
	})
	t.Run("outside the workspace is rejected", func(t *testing.T) {
		assert.ErrMsgContains(t, edit(map[string]any{"path": "../x.go", "old_string": "a", "new_string": "b"}), "path escape")
	})
}
