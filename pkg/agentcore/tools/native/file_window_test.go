package native

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suag/pkg/agentcore/api"
)

func execTool(t *testing.T, tool interface {
	Execute(context.Context, api.ToolCall) (*api.ToolResult, error)
}, args map[string]any) string {
	t.Helper()
	res, err := tool.Execute(context.Background(), api.ToolCall{CallID: "c", Arguments: args})
	assert.Require(t, assert.NoErr(t, err))
	return res.Content
}

var lineNoPrefix = regexp.MustCompile(`(?m)^\d+\t`)
var nextOffset = regexp.MustCompile(`continue with offset=(\d+)\]$`)

func TestFileReadWindow(t *testing.T) {
	ws := t.TempDir()
	var sb strings.Builder
	for i := 1; i <= 250; i++ {
		fmt.Fprintf(&sb, "第%d行 line %d\n", i, i)
	}
	original := sb.String()
	assert.NoErr(t, os.WriteFile(filepath.Join(ws, "big.txt"), []byte(original), 0o644))
	tool := &FileReadTool{workspace: ws, maxOut: 500}

	t.Run("reading on with offset rebuilds the file", func(t *testing.T) {
		var got strings.Builder
		offset, calls := 1, 0
		for {
			calls++
			assert.Require(t, assert.True(t, calls < 100))
			out := execTool(t, tool, map[string]any{"path": "big.txt", "offset": offset, "limit": 40})
			m := nextOffset.FindStringSubmatch(out)
			body := out
			if m != nil {
				body = out[:strings.LastIndex(out, "[truncated")]
			}
			got.WriteString(strings.TrimRight(lineNoPrefix.ReplaceAllString(body, ""), "\n") + "\n")
			if m == nil {
				break
			}
			fmt.Sscan(m[1], &offset)
		}
		assert.Eq(t, original, got.String())
		assert.True(t, calls > 250/40+1, "calls %d", calls) // the byte cap cut windows short
	})
	t.Run("first line carries its number", func(t *testing.T) {
		out := execTool(t, tool, map[string]any{"path": "big.txt", "offset": "3", "limit": 1})
		assert.True(t, strings.HasPrefix(out, "3\t第3行 line 3\n[truncated at limit 1 lines; continue with offset=4]"), out)
	})
	t.Run("offset past the end", func(t *testing.T) {
		assert.StrContains(t, execTool(t, tool, map[string]any{"path": "big.txt", "offset": 999}), "has 250 lines")
	})
	t.Run("binary file is reported", func(t *testing.T) {
		assert.NoErr(t, os.WriteFile(filepath.Join(ws, "a.bin"), []byte{0x89, 'P', 'N', 'G', 0, 1, 2}, 0o644))
		out := execTool(t, tool, map[string]any{"path": "a.bin"})
		assert.StrContains(t, out, "[binary file: a.bin, 7 bytes")
	})
	t.Run("CRLF lines are shown without CR", func(t *testing.T) {
		assert.NoErr(t, os.WriteFile(filepath.Join(ws, "crlf.txt"), []byte("a\r\nb\r\n"), 0o644))
		assert.Eq(t, "1\ta\n2\tb", execTool(t, tool, map[string]any{"path": "crlf.txt"}))
	})
	t.Run("one huge line returns a part", func(t *testing.T) {
		assert.NoErr(t, os.WriteFile(filepath.Join(ws, "long.txt"), []byte(strings.Repeat("中", 1000)+"\nnext\n"), 0o644))
		out := execTool(t, tool, map[string]any{"path": "long.txt"})
		assert.StrContains(t, out, "continue with offset=2]")
	})
}

func TestFileListDepth(t *testing.T) {
	ws := t.TempDir()
	for _, p := range []string{"a/b/c/d.txt", "a/x.go", "node_modules/m/i.js", "top.md"} {
		assert.NoErr(t, os.MkdirAll(filepath.Join(ws, filepath.Dir(p)), 0o755))
		assert.NoErr(t, os.WriteFile(filepath.Join(ws, p), []byte("x"), 0o644))
	}
	tool := &FileListTool{workspace: ws}

	t.Run("default is one level", func(t *testing.T) {
		assert.Eq(t, "a/\nnode_modules/\ntop.md", execTool(t, tool, map[string]any{}))
	})
	t.Run("depth recurses and skips ignored dirs", func(t *testing.T) {
		out := execTool(t, tool, map[string]any{"depth": 5})
		assert.Eq(t, "a/\na/b/\na/b/c/\na/b/c/d.txt\na/x.go\nnode_modules/\ntop.md", out)
	})
	t.Run("max_entries truncates", func(t *testing.T) {
		out := execTool(t, tool, map[string]any{"depth": 5, "max_entries": 2})
		assert.Eq(t, "a/\na/b/\n[truncated at 2 entries; narrow dir or depth]", out)
	})
	t.Run("symlinks are marked and not followed", func(t *testing.T) {
		outside := t.TempDir()
		assert.NoErr(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s"), 0o644))
		if err := os.Symlink(outside, filepath.Join(ws, "link")); err != nil {
			t.Skip("symlink not supported:", err)
		}
		out := execTool(t, tool, map[string]any{"depth": 3})
		assert.StrContains(t, out, "link@")
		assert.NotContains(t, out, "secret.txt")
	})
}
