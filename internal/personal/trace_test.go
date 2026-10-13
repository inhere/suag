package personal

import (
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suag/internal/personal/store"
)

// TestTraceAgentLines: a sub-agent call line shows the session it ran in, its
// type, its steps (with the sub-agent's own tool calls) and the reason it
// stopped, without repeating the calls= part; the start of a background call
// has none of these numbers (they are on its task's end event).
func TestTraceAgentLines(t *testing.T) {
	t.Run("a synchronous call shows what the sub-agent spent", func(t *testing.T) {
		line := traceLine(store.Event{Kind: store.EventTool, Name: "agent", Status: eventOK, DurationMs: 41300,
			Attrs: map[string]any{"arg": "description=find the callers type=explore", "child_session_id": "sess_child",
				"agent_type": "explore", "steps": float64(12), "tool_calls": float64(5), "input_tokens": float64(85000),
				"cache_read_tokens": float64(60000), "output_tokens": float64(3000), "cost_usd": 0.0123, "stop": "final"}})
		assert.StrContains(t, line, "tool agent ok 41300ms sub=sess_child type=explore steps=12 calls=5 "+
			"in=85k(cache 60k) out=3k cost=$0.0123 stop=final [description=find the callers type=explore]")
		assert.Eq(t, 1, strings.Count(line, "calls="))
	})

	t.Run("a background call shows the type it started with", func(t *testing.T) {
		line := traceLine(store.Event{Kind: store.EventTool, Name: "agent", Status: eventOK, DurationMs: 6,
			Attrs: map[string]any{"arg": "background=true description=survey deps", "task_id": "t4", "agent_type": "research"}})
		assert.StrContains(t, line, "tool agent ok 6ms type=research [background=true description=survey deps]")
	})

	t.Run("the task of a background call is an agent task with its steps", func(t *testing.T) {
		line := traceLine(store.Event{Kind: store.EventTask, Name: "t4", Status: eventOK, DurationMs: 20000,
			Attrs: map[string]any{"kind": "agent", "arg": "agent: survey deps", "steps": float64(7),
				"tool_calls": float64(3), "exit_code": float64(0)}})
		assert.StrContains(t, line, "task t4 ok 20000ms agent steps=7 calls=3 exit=0 [agent: survey deps]")
		assert.Eq(t, 1, strings.Count(line, "calls="))
	})
}

// TestTraceTaskLines: a task end event line shows the task's kind — a watch
// with its runs and changes (or its last state when no counters arrived) and a
// background Stop hook by its summary — while a shell task and events of
// older versions keep the plain exit code and command bracket.
func TestTraceTaskLines(t *testing.T) {
	task := func(attrs map[string]any) store.Event {
		return store.Event{Kind: store.EventTask, Name: "t3", Status: eventOK, DurationMs: 1200, Attrs: attrs}
	}

	t.Run("a shell task keeps the command bracket", func(t *testing.T) {
		line := traceLine(task(map[string]any{"kind": "shell", "arg": "go test ./... -count=1", "exit_code": float64(1)}))
		assert.StrContains(t, line, "task t3 ok 1200ms exit=1 [go test ./... -count=1]")
	})

	t.Run("a watch shows its runs and changes", func(t *testing.T) {
		line := traceLine(task(map[string]any{"kind": "watch", "arg": "go test ./... (every 30s)", "exit_code": float64(1),
			"runs": float64(5), "changes": float64(2), "progress": "run 5, last exit 1, 2 changes"}))
		assert.StrContains(t, line, "task t3 ok 1200ms watch runs=5 changes=2 exit=1 [go test ./... (every 30s)]")
	})

	t.Run("a watch without counters shows its last state", func(t *testing.T) {
		line := traceLine(task(map[string]any{"kind": "watch", "arg": "go build ./...",
			"exit_code": float64(0), "progress": "run 1, last exit 0, 0 changes"}))
		assert.StrContains(t, line, "watch progress=run 1, last exit 0, 0 changes exit=0 [go build ./...]")
	})

	t.Run("a Stop hook is named by its summary", func(t *testing.T) {
		line := traceLine(task(map[string]any{"kind": "hook", "arg": "Stop hook", "exit_code": float64(0)}))
		assert.StrContains(t, line, "task t3 ok 1200ms Stop hook exit=0")
		assert.NotContains(t, line, "[Stop hook]")
	})

	t.Run("an event without a kind renders as before", func(t *testing.T) {
		line := traceLine(task(map[string]any{"arg": "go test ./...", "exit_code": float64(0)}))
		assert.StrContains(t, line, "task t3 ok 1200ms exit=0 [go test ./...]")
	})
}
