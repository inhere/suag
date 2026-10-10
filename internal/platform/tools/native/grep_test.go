package native

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suag/pkg/agentcore/api"
)

// searchWorkspace builds a small tree for the grep/find tests.
func searchWorkspace(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	files := map[string]string{
		"main.go":                 "package main\n\nfunc main() { Hello() }\n",
		"internal/a/a.go":         "package a\n\n// Hello says hi\nfunc Hello() {}\n",
		"internal/a/a_test.go":    "package a\n\nfunc TestHello(t *testing.T) {}\n",
		"docs/中文.md":              "# 标题\n你好 Hello 世界\n",
		"node_modules/x/index.js": "Hello()\n",
		"build/out.go":            "func Hello() {}\n",
		"logs/app.log":            "Hello log\n",
		".gitignore":              "# comment\nbuild/\n*.log\n!keep.log\n",
	}
	for p, c := range files {
		full := filepath.Join(ws, filepath.FromSlash(p))
		assert.NoErr(t, os.MkdirAll(filepath.Dir(full), 0o755))
		assert.NoErr(t, os.WriteFile(full, []byte(c), 0o644))
	}
	assert.NoErr(t, os.WriteFile(filepath.Join(ws, "bin.dat"), []byte("Hello\x00\x01binary"), 0o644))
	return ws
}

func TestGrepFiles(t *testing.T) {
	ws := searchWorkspace(t)
	tool := &FileGrepTool{workspace: ws}

	t.Run("content with ignores and binary skipped", func(t *testing.T) {
		out := execTool(t, tool, map[string]any{"pattern": "Hello"})
		assert.StrContains(t, out, "internal/a/a.go:3: // Hello says hi")
		assert.StrContains(t, out, "main.go:3: func main() { Hello() }")
		assert.StrContains(t, out, "docs/中文.md:2: 你好 Hello 世界")
		for _, skipped := range []string{"node_modules", "build/", "app.log", "bin.dat"} {
			assert.NotContains(t, out, skipped)
		}
	})
	t.Run("files output, glob and ignore_case", func(t *testing.T) {
		out := execTool(t, tool, map[string]any{"pattern": "hello", "ignore_case": true, "glob": "internal/**/*.go", "output": "files"})
		assert.Eq(t, "internal/a/a.go\ninternal/a/a_test.go", out)
	})
	t.Run("path narrows the search", func(t *testing.T) {
		out := execTool(t, tool, map[string]any{"pattern": "Hello", "path": "docs"})
		assert.Eq(t, "docs/中文.md:2: 你好 Hello 世界", out)
	})
	t.Run("a single file path", func(t *testing.T) {
		out := execTool(t, tool, map[string]any{"pattern": "func", "path": "main.go"})
		assert.Eq(t, "main.go:3: func main() { Hello() }", out)
	})
	t.Run("max_results stops early", func(t *testing.T) {
		out := execTool(t, tool, map[string]any{"pattern": "Hello", "max_results": 1})
		assert.StrContains(t, out, "[stopped at 1 results")
		assert.Eq(t, 2, len(strings.Split(out, "\n")))
	})
	t.Run("no matches", func(t *testing.T) {
		assert.Eq(t, "no matches", execTool(t, tool, map[string]any{"pattern": "zzz_nothing"}))
	})
	t.Run("timeout returns a marked partial result", func(t *testing.T) {
		old := searchTimeout
		searchTimeout = time.Nanosecond
		t.Cleanup(func() { searchTimeout = old })
		assert.StrContains(t, execTool(t, tool, map[string]any{"pattern": "Hello"}), "已超时")
	})
	t.Run("symlink out of the workspace is not read", func(t *testing.T) {
		outside := t.TempDir()
		assert.NoErr(t, os.WriteFile(filepath.Join(outside, "secret.go"), []byte("Hello secret\n"), 0o644))
		if err := os.Symlink(outside, filepath.Join(ws, "link")); err != nil {
			t.Skip("symlink not supported:", err)
		}
		assert.NoErr(t, os.Symlink(filepath.Join(outside, "secret.go"), filepath.Join(ws, "flink.go")))
		out := execTool(t, tool, map[string]any{"pattern": "secret"})
		assert.Eq(t, "no matches", out)
		_, err := tool.Execute(t.Context(), callWith(map[string]any{"pattern": "secret", "path": "link"}))
		assert.Err(t, err)
	})
	t.Run("invalid regexp", func(t *testing.T) {
		_, err := tool.Execute(t.Context(), callWith(map[string]any{"pattern": "("}))
		assert.ErrMsgContains(t, err, "invalid pattern")
	})
}

func TestFindFiles(t *testing.T) {
	ws := searchWorkspace(t)
	tool := &FileFindTool{workspace: ws}
	old := time.Now().Add(-time.Hour)
	assert.NoErr(t, os.Chtimes(filepath.Join(ws, "main.go"), old, old))

	t.Run("base-name pattern at any depth, newest first", func(t *testing.T) {
		out := execTool(t, tool, map[string]any{"pattern": "*.go"})
		lines := strings.Split(out, "\n")
		assert.Eq(t, 3, len(lines))
		assert.Eq(t, "main.go", lines[2]) // oldest last; build/ is ignored
	})
	t.Run("double star", func(t *testing.T) {
		assert.Eq(t, "internal/a/a_test.go", execTool(t, tool, map[string]any{"pattern": "**/*_test.go"}))
	})
	t.Run("limit", func(t *testing.T) {
		assert.StrContains(t, execTool(t, tool, map[string]any{"pattern": "*.go", "max_results": 1}), "[showing 1 of 3")
	})
	t.Run("nothing found", func(t *testing.T) {
		assert.Eq(t, "no files found", execTool(t, tool, map[string]any{"pattern": "*.rs"}))
	})
}

func TestWalkGlobMatch(t *testing.T) {
	for _, tc := range []struct {
		pattern, rel string
		want         bool
	}{
		{"*.go", "a/b/c.go", true},
		{"**/*.go", "c.go", true},
		{"internal/**/*.go", "internal/x/y/z.go", true},
		{"internal/**/*.go", "cmd/z.go", false},
		{"cmd/*/main.go", "cmd/suag/main.go", true},
		{"cmd/*/main.go", "cmd/a/b/main.go", false},
	} {
		assert.Eq(t, tc.want, globMatch(tc.pattern, tc.rel), tc.pattern+" "+tc.rel)
	}
}

func callWith(args map[string]any) api.ToolCall {
	return api.ToolCall{CallID: "c", Arguments: args}
}
