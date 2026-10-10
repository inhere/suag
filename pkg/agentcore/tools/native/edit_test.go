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
	t.Run("outside the workspace is rejected", func(t *testing.T) {
		assert.ErrMsgContains(t, edit(map[string]any{"path": "../x.go", "old_string": "a", "new_string": "b"}), "path escape")
	})
}
