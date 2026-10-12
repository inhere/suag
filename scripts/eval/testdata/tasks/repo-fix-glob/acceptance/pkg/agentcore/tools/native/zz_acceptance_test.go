package native

// Acceptance test injected after the run (the agent does not see it).

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suag/pkg/agentcore/api"
)

func TestAcceptGlobRelativeToSearchPath(t *testing.T) {
	ws, dep := readRootFixture(t)
	assert.NoErr(t, os.MkdirAll(filepath.Join(ws, "internal", "a"), 0o755))
	assert.NoErr(t, os.WriteFile(filepath.Join(ws, "internal", "a", "x.go"), []byte("package a // Hello\n"), 0o644))
	grep := &FileGrepTool{workspace: ws, readRoots: []string{dep}}
	find := &FileFindTool{workspace: ws, readRoots: []string{dep}}

	t.Run("glob relative to path, under a read root", func(t *testing.T) {
		out := execTool(t, grep, map[string]any{"pattern": "Exported", "path": dep, "glob": "mod/*.go"})
		assert.StrContains(t, out, "lib.go:3:")
		out = execTool(t, grep, map[string]any{"pattern": "Exported", "path": filepath.Join(dep, "mod"), "glob": "*.go"})
		assert.StrContains(t, out, "lib.go:3:")
		out = execTool(t, find, map[string]any{"pattern": "mod/*.go", "path": dep})
		assert.StrContains(t, out, "lib.go")
	})
	t.Run("glob relative to path or to the workspace", func(t *testing.T) {
		assert.StrContains(t, execTool(t, grep, map[string]any{"pattern": "Hello", "path": "internal", "glob": "a/*.go"}), "internal/a/x.go:1:")
		assert.StrContains(t, execTool(t, grep, map[string]any{"pattern": "Hello", "path": "internal", "glob": "internal/a/*.go"}), "internal/a/x.go:1:")
		assert.StrContains(t, execTool(t, find, map[string]any{"pattern": "a/x.go", "path": "internal"}), "internal/a/x.go")
	})
	t.Run("a missing search path says so", func(t *testing.T) {
		_, err := find.Execute(context.Background(), api.ToolCall{CallID: "c", Arguments: map[string]any{"pattern": "*.go", "path": filepath.Join(dep, "nope")}})
		assert.ErrMsgContains(t, err, "does not exist")
	})
}
