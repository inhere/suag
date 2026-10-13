package personal

import (
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suag/internal/personal/store"
)

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
