package personal

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// TestTraceWebRenders runs the page script of traceweb.html with node against
// a minimal DOM (testdata/traceweb_check.js): the environment tab and the
// event details render text, never "[object ...]". Skipped without node.
func TestTraceWebRenders(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}
	repo, sid := traceRun(t)
	view, err := BuildTraceView(context.Background(), repo, sid)
	assert.Require(t, assert.NoErr(t, err))
	dir := t.TempDir()
	write := func(name string, v any) string {
		data, err := json.Marshal(v)
		assert.Require(t, assert.NoErr(t, err))
		p := filepath.Join(dir, name)
		assert.Require(t, assert.NoErr(t, os.WriteFile(p, data, 0o600)))
		return p
	}
	html := filepath.Join(dir, "traceweb.html")
	assert.Require(t, assert.NoErr(t, os.WriteFile(html, traceWebHTML, 0o600)))
	// The shape of wiring.LocalEnv the page reads.
	env := map[string]any{
		"workspace": "/w",
		"manifest": map[string]any{
			"version": "dev", "models": map[string]any{"main": map[string]any{"provider": "p", "model": "m"}},
			"tools":  []any{map[string]any{"name": "file_read", "read_only": true}, map[string]any{"name": "shell_exec", "needs_allow_write": true}},
			"memory": []any{map[string]any{"scope": "user", "backend": "files"}},
		},
		"tools":        []any{map[string]any{"name": "file_read", "description": "read"}, map[string]any{"name": "shell_exec", "description": "run"}},
		"skills":       []any{map[string]any{"name": "go-dev", "shown": "listed"}, map[string]any{"name": "pdf", "shown": "named"}},
		"skills_stats": map[string]any{"listed": 1, "named": 1, "preloaded": 0, "total": 2},
		"skills_text":  "# Skills",
		"rules":        map[string]any{"file": "AGENTS.md", "files": []any{map[string]any{"path": "/w/AGENTS.md", "exists": true, "bytes": 10}}, "hash": "abc"},
		"hooks":        map[string]any{"enabled": false},
		"mcp_servers":  []any{map[string]any{"id": "srv", "command": "srv"}},
	}
	out, err := exec.Command(node, filepath.Join("testdata", "traceweb_check.js"), html, write("session.json", view), write("env.json", env)).CombinedOutput()
	t.Log(string(out))
	assert.NoErr(t, err)
	assert.NotContains(t, string(out), "FAIL")
}
