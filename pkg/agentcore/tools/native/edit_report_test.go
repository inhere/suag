package native

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suag/pkg/agentcore/api"
)

func runEdit(t *testing.T, ws string, args map[string]any) (string, error) {
	t.Helper()
	res, err := (&FileEditTool{workspace: ws}).Execute(context.Background(), api.ToolCall{CallID: "c", Arguments: args})
	if err != nil {
		return "", err
	}
	return res.Content, nil
}

func numbered(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "L%02d\n", i)
	}
	return b.String()
}

func TestFileEditResultShowsEditedLines(t *testing.T) {
	ws := t.TempDir()
	write := func(name, text string) {
		assert.NoErr(t, os.WriteFile(filepath.Join(ws, name), []byte(text), 0o644))
	}
	t.Run("one edit shows its lines with context", func(t *testing.T) {
		write("a.txt", "one\ntwo\nthree\nfour\nfive\nsix\nseven\n")
		out, err := runEdit(t, ws, map[string]any{"path": "a.txt", "old_string": "four", "new_string": "FOUR\nFOUR-B"})
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, out, "1 replacement(s)")
		assert.StrContains(t, out, "2\ttwo\n3\tthree\n4\tFOUR\n5\tFOUR-B\n6\tfive\n7\tsix")
		assert.NotContains(t, out, "1\tone")
	})
	t.Run("several edits: earlier spans follow later changes", func(t *testing.T) {
		write("b.txt", numbered(40))
		out, err := runEdit(t, ws, map[string]any{"path": "b.txt", "edits": []any{
			map[string]any{"old_string": "L02\n", "new_string": "L02\nINSERTED\n"},
			map[string]any{"old_string": "L20\n", "new_string": "CHANGED\n"},
			map[string]any{"old_string": "L01\n", "new_string": strings.Repeat("XX\n", 10) + "X2\nL01\n"}, // shifts both earlier spans by 11 lines
		}})
		assert.Require(t, assert.NoErr(t, err))
		data, _ := os.ReadFile(filepath.Join(ws, "b.txt"))
		lines := strings.Split(string(data), "\n")
		for _, want := range []string{"INSERTED", "CHANGED", "X2"} {
			n := 0
			for i, l := range lines {
				if l == want {
					n = i + 1
				}
			}
			assert.Require(t, assert.True(t, n > 0, want))
			assert.StrContains(t, out, fmt.Sprintf("%d\t%s", n, want))
		}
		assert.StrContains(t, out, "...")   // separate regions
		assert.NotContains(t, out, "\tL12") // far from every edit
	})
	t.Run("replace_all, deletion and CRLF", func(t *testing.T) {
		write("c.txt", "a\r\nkey=1\r\nb\r\nc\r\nd\r\ne\r\nf\r\nkey=1\r\ng\r\n")
		out, err := runEdit(t, ws, map[string]any{"path": "c.txt", "old_string": "key=1", "new_string": "key=2", "replace_all": true})
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, out, "2 replacement(s)")
		assert.StrContains(t, out, "2\tkey=2")
		assert.StrContains(t, out, "8\tkey=2")
		out, err = runEdit(t, ws, map[string]any{"path": "c.txt", "old_string": "c\nd\n", "new_string": ""})
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, out, "3\tb\n4\te") // around the deletion point
	})
	t.Run("a Go syntax error introduced by the edit is flagged", func(t *testing.T) {
		write("m.go", "package m\n\nfunc F() int {\n\treturn 1\n}\n")
		out, err := runEdit(t, ws, map[string]any{"path": "m.go", "old_string": "return 1\n}", "new_string": "return 1\n"})
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, out, "warning: this edit introduced a Go syntax error at line")
		out, err = runEdit(t, ws, map[string]any{"path": "m.go", "old_string": "return 1\n", "new_string": "return 2\n}\n"})
		assert.Require(t, assert.NoErr(t, err))
		assert.NotContains(t, out, "warning") // fixed again
	})
}

func TestFileEditFailureHints(t *testing.T) {
	ws := t.TempDir()
	assert.NoErr(t, os.WriteFile(filepath.Join(ws, "s.go"), []byte("package s\n\nfunc Start(ctx context.Context) error {\n\t\treturn nil\n}\n\nfunc Stop() {}\nfunc Stop2() {}\n"), 0o644))
	t.Run("same text with other indentation", func(t *testing.T) {
		_, err := runEdit(t, ws, map[string]any{"path": "s.go", "old_string": "  return nil\n", "new_string": "  return ctx.Err()\n"})
		assert.ErrMsgContains(t, err, "old_string not found")
		assert.ErrMsgContains(t, err, "appears at line 4 with different whitespace")
		assert.ErrMsgContains(t, err, "4\t\t\treturn nil")
	})
	t.Run("a similar line is named", func(t *testing.T) {
		_, err := runEdit(t, ws, map[string]any{"path": "s.go", "old_string": "func Start(ctx context.Context) (err error) {", "new_string": "x"})
		assert.ErrMsgContains(t, err, "the closest line is 3")
	})
	t.Run("nothing close", func(t *testing.T) {
		_, err := runEdit(t, ws, map[string]any{"path": "s.go", "old_string": "zzz qqq", "new_string": "x"})
		assert.ErrMsgContains(t, err, "re-read the part you are editing")
	})
	t.Run("several matches list their lines", func(t *testing.T) {
		_, err := runEdit(t, ws, map[string]any{"path": "s.go", "old_string": "func Stop", "new_string": "func Halt"})
		assert.ErrMsgContains(t, err, "matches 2 places (lines 7, 8)")
	})
}
