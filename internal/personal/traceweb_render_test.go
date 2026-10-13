package personal

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suag/internal/personal/store"
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
	// P2: a background task of that turn — the shell_exec that started it, the
	// task's end event (stored when the task ended, so after the wait call that
	// came later) and the notice entry the model got in place of a person's
	// input.
	start := view.Events[0].Start
	for _, e := range view.Events {
		if e.Start.Before(start) {
			start = e.Start
		}
	}
	turn := view.Events[0].TurnID
	view.Events = append(view.Events,
		webEvent{ID: 9001, TurnID: turn, Kind: store.EventTool, Name: "shell_exec", Status: eventOK,
			Start: start.Add(50 * time.Millisecond), DurationMs: 20,
			Attrs: map[string]any{"arg": "command=go test ./... -count=1", "task_id": "t1", "exit_code": 0}},
		webEvent{ID: 9003, TurnID: turn, Kind: store.EventTool, Name: "task", Status: eventOK,
			Start: start.Add(1300 * time.Millisecond), DurationMs: 10, Attrs: map[string]any{"arg": "action=wait id=t1"}},
		webEvent{ID: 9002, TurnID: turn, Kind: store.EventTask, Name: "t1", Status: eventOK,
			Start: start.Add(80 * time.Millisecond), DurationMs: 1200,
			Attrs: map[string]any{"arg": "go test ./... -count=1", "exit_code": 0, "bytes": 1200, "lines": 30}},
		// P3: a watch (its counters and its starting shell_exec) and a background
		// Stop hook (named by its summary). Their rows start after the shell task's.
		webEvent{ID: 9011, TurnID: turn, Kind: store.EventTool, Name: "shell_exec", Status: eventOK,
			Start: start.Add(100 * time.Millisecond), DurationMs: 15,
			Attrs: map[string]any{"arg": "command=go vet ./... every=30 watch_paths=**/*.go", "task_id": "t2", "exit_code": 0}},
		webEvent{ID: 9012, TurnID: turn, Kind: store.EventTask, Name: "t2", Status: eventOK,
			Start: start.Add(100 * time.Millisecond), DurationMs: 30000,
			Attrs: map[string]any{"kind": "watch", "arg": "go vet ./... (every 30s and on changes of **/*.go)",
				"exit_code": 1, "runs": 5, "changes": 2, "progress": "run 5, last exit 1, 2 changes", "bytes": 800, "lines": 12}},
		webEvent{ID: 9013, TurnID: turn, Kind: store.EventTask, Name: "t3", Status: eventOK,
			Start: start.Add(2000 * time.Millisecond), DurationMs: 2100,
			Attrs: map[string]any{"kind": "hook", "arg": "Stop hook", "exit_code": 0, "bytes": 12, "lines": 2}})
	view.Entries = append(view.Entries, traceEntry{Seq: 9004, TurnID: turn, Type: store.EntryUser, Role: "user",
		Time: start.Add(1400 * time.Millisecond), Content: "[background task notice] t1 ended", Notice: true})
	// A running task's report (a watch whose result changed) is stored as a
	// notice entry too and must not read as the person's input.
	view.Entries = append(view.Entries, traceEntry{Seq: 9014, TurnID: turn, Type: store.EntryUser, Role: "user",
		Time: start.Add(1500 * time.Millisecond), Notice: true,
		Content: "[background task notice] Background task updates (read more with the task tool or recall the ref):\n" +
			"- t2 (watch) reports: run 5 (files: a.go): exit 0 -> 1"})
	// The 分析 tab shows the numbers of the events the page got.
	traceEvents := make([]store.Event, 0, len(view.Events))
	for _, e := range view.Events {
		traceEvents = append(traceEvents, store.Event{Kind: e.Kind, Name: e.Name, Status: e.Status,
			StartedAt: e.Start, DurationMs: e.DurationMs, Attrs: e.Attrs})
	}
	view.Analysis = AnalyzeTrace(traceEvents)
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
