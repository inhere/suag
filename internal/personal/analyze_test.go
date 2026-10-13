package personal

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/mock"
	"github.com/inhere/suag/pkg/agentcore/tools"
)

func TestAnalyzeTrace(t *testing.T) {
	model := func(calls int, ms int64) store.Event {
		return store.Event{Kind: store.EventModel, Status: eventOK, DurationMs: ms, OutputTokens: 10,
			InputTokens: 100, CacheReadTokens: 80, Attrs: map[string]any{"tool_calls": float64(calls), "reasoning_chars": float64(50)}}
	}
	tool := func(name, arg string, target []any, code any) store.Event {
		attrs := map[string]any{"arg": arg}
		if target != nil {
			attrs["target"] = target
		}
		if code != nil {
			attrs["exit_code"] = code
		}
		return store.Event{Kind: store.EventTool, Name: name, Status: eventOK, Attrs: attrs}
	}
	events := []store.Event{
		model(2, 1000),
		tool("file_read", "paths=[a.go b.go]", []any{"a.go", "b.go"}, nil),
		tool("file_list", "dir=.", []any{"."}, nil),
		model(1, 5000),
		tool("file_write", "path=a.go", []any{"a.go"}, nil),
		model(1, 2000),
		tool("shell_exec", "command=go test ./...", nil, float64(1)),
		model(1, 1000),
		tool("file_edit", "path=a.go", []any{"a.go"}, nil),
		model(1, 1000),
		tool("shell_exec", "command=go test ./...", nil, float64(0)),
		model(1, 1000),
		tool("shell_exec", "command=go test -count=3 ./...", nil, float64(0)),
		model(1, 1000),
		tool("file_read", "path=a.go", []any{"a.go"}, nil),
		{Kind: store.EventFold, Name: "fold", Status: eventOK},
		{Kind: store.EventModel, Status: eventOK, Attrs: map[string]any{"purpose": "compact"}}, // not a step
		model(0, 9000),
	}
	a := AnalyzeTrace(events)
	assert.Eq(t, 8, a.ModelCalls)
	assert.Eq(t, 8, a.ToolCalls)
	assert.Eq(t, 1, a.ParallelSteps)
	assert.Eq(t, 2, a.MaxCallsPerStep)
	assert.Eq(t, []Count{{Name: "a.go", Count: 2}}, a.Rereads)
	assert.Eq(t, 1, a.ReadsAfterWrite)
	assert.Eq(t, []Count{{Name: "a.go", Count: 2}}, a.EditsPerFile)
	assert.Eq(t, 3, a.ShellRuns)
	assert.Eq(t, 1, a.ShellFailures)
	assert.Eq(t, []Count{{Name: "command=go test ./...", Count: 2}}, a.RepeatedCommands)
	assert.Eq(t, 3, a.TestRuns)
	assert.Eq(t, 1, a.TestRunsAfterPass) // the -count=3 run after the first pass
	assert.Eq(t, 1, a.Folds)
	assert.Eq(t, 400, a.ReasoningChars)
	// steps: 1 read+list, 2 write, 3 failing test, 4 edit, 5 passing test, 6 test, 7 read, 8 answer
	assert.Eq(t, 1, a.ExploreSteps)
	assert.Eq(t, 4, a.WriteToGreenSteps)
	assert.Eq(t, 3, a.AfterGreenSteps)
	assert.Eq(t, 2, a.EditSteps)
	assert.Eq(t, 2, a.SingleEditSteps)
	assert.Eq(t, int64(9000), a.Slowest[0].DurationMs)
	assert.Eq(t, int64(21000), a.ModelMs) // the compaction call is not counted

	var out bytes.Buffer
	assert.NoErr(t, WriteTraceAnalysis(&out, a, false))
	for _, want := range []string{
		"8 model calls, 8 tool calls, 1 parallel steps (max 2 calls in one step)",
		"re-reads               a.go x2",
		"reads after own write  1",
		"tests                  3 runs, 1 after the first pass",
		"phases                 explore 1, first write to green 4, after green 3 (model calls)",
		"edit steps             2, 2 with a single change",
		"slowest model calls    step 8 9.0s",
	} {
		assert.StrContains(t, out.String(), want)
	}
}

// TestToolEventsCarryCallAttrs: tool events record arg, target and the shell
// exit code; model events the number of tool calls.
func TestToolEventsCarryCallAttrs(t *testing.T) {
	repo := openObserveRepo(t)
	registry := tools.NewRegistry()
	registry.Register(fnTool{name: "shell_exec", fn: func() (*agentcore.ToolResult, error) {
		return &agentcore.ToolResult{Content: "FAIL\n[exit code: 1]"}, nil
	}})
	registry.Register(fnTool{name: "file_read", fn: func() (*agentcore.ToolResult, error) {
		return &agentcore.ToolResult{Content: "1\tx"}, nil
	}})
	runner, err := New(Config{Repository: repo, Tools: registry, Provider: mock.NewProvider(
		mock.ToolCall("c1", "shell_exec", map[string]any{"command": "go test ./... --token=sk-abcdefghijklmnopqrstuvwxyz123456"}),
		mock.ToolCall("c2", "file_read", map[string]any{"paths": []any{"a.go", "dir/b.go"}}),
		mock.Final("done"),
	)})
	assert.NoErr(t, err)
	res, err := runner.Run(context.Background(), RunRequest{Prompt: "go"})
	assert.Require(t, assert.NoErr(t, err))

	events := eventsOf(t, repo, res.SessionID, store.EventTool)
	assert.Require(t, assert.Eq(t, 2, len(events)))
	assert.Eq(t, float64(1), events[0].Attrs["exit_code"])
	assert.StrContains(t, events[0].Attrs["arg"].(string), "command=go test ./...")
	assert.NotContains(t, events[0].Attrs["arg"].(string), "sk-abcdefghijklmnopqrstuvwxyz123456") // redacted
	assert.Eq(t, []any{"a.go", "dir/b.go"}, events[1].Attrs["target"])
	models := eventsOf(t, repo, res.SessionID, store.EventModel)
	assert.Eq(t, float64(1), models[0].Attrs["tool_calls"])
	// Every request of the run is compared with the one before (cache attribution).
	assert.Require(t, assert.Eq(t, 3, len(models)))
	assert.Eq(t, prefixFirst, models[0].Attrs["prefix"])
	assert.Eq(t, prefixAppend, models[1].Attrs["prefix"])
	assert.Eq(t, prefixAppend, models[2].Attrs["prefix"])
	assert.Gt(t, models[2].Attrs["prefix_reused"].(float64), models[1].Attrs["prefix_reused"].(float64))
	assert.StrContains(t, traceLine(events[0]), "exit=1")

	turns := eventsOf(t, repo, res.SessionID, store.EventTurn)
	assert.Require(t, assert.Len(t, turns, 1))
	assert.Eq(t, float64(2), turns[0].Attrs["prompt_chars"]) // "go"
	assert.Nil(t, turns[0].Attrs["injected"])

	a := AnalyzeTrace(eventsOf(t, repo, res.SessionID))
	assert.Eq(t, 1, a.ShellFailures)
	assert.Eq(t, 1, a.TestRuns)
}

// TestAnalyzeTraceBackground covers the background task numbers: the overlap
// of the task runs with foreground work, the task tool's waits and the waits
// that came right after a task was started.
func TestAnalyzeTraceBackground(t *testing.T) {
	base := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	at := func(ms int64) time.Time { return base.Add(time.Duration(ms) * time.Millisecond) }
	// Events are built with explicit times: the task end events are stored when
	// the tasks end, so their place in the list is unrelated to their run.
	model := func(startMs, durMs int64) store.Event {
		return store.Event{Kind: store.EventModel, Status: eventOK, StartedAt: at(startMs), DurationMs: durMs, Attrs: map[string]any{}}
	}
	tool := func(name string, startMs, durMs int64, attrs map[string]any) store.Event {
		if attrs == nil {
			attrs = map[string]any{}
		}
		return store.Event{Kind: store.EventTool, Name: name, Status: eventOK, StartedAt: at(startMs), DurationMs: durMs, Attrs: attrs}
	}
	task := func(id string, startMs, durMs int64) store.Event {
		return store.Event{Kind: store.EventTask, Name: id, Status: eventOK, StartedAt: at(startMs), DurationMs: durMs, Attrs: map[string]any{}}
	}
	background := func(startMs, durMs int64, id string) store.Event {
		return tool("shell_exec", startMs, durMs, map[string]any{"arg": "command=go test ./...", "task_id": id})
	}

	t.Run("overlap with model and tool calls that overlap each other", func(t *testing.T) {
		a := AnalyzeTrace([]store.Event{
			model(0, 1000), // 0-1000
			tool("file_read", 1000, 500, nil),
			tool("file_grep", 1200, 800, nil), // covers 1200-2000, overlaps the read
			task("t1", 0, 3000),
		})
		assert.Eq(t, 1, a.BackgroundTasks)
		assert.Eq(t, int64(3000), a.BackgroundMs)
		// 0-1000 model plus the merged 1000-2000 of the two parallel calls.
		assert.Eq(t, int64(2000), a.BackgroundOverlapMs)
		assert.Eq(t, 0, a.TaskWaits)
		assert.Eq(t, 0, a.ImmediateWaits)
	})

	t.Run("a wait does not count as saved time", func(t *testing.T) {
		a := AnalyzeTrace([]store.Event{
			model(0, 1000),
			background(1000, 10, "t1"),
			tool("task", 2000, 3000, map[string]any{"arg": "action=wait id=t1"}), // 2000-5000, blocks the loop
			task("t1", 1000, 5000), // 1000-6000
			tool("task", 6500, 10, map[string]any{"arg": "action=output id=t1"}),
			model(7000, 1000),
		})
		assert.Eq(t, 1, a.BackgroundTasks)
		assert.Eq(t, 1, a.TaskWaits)
		assert.Eq(t, int64(3000), a.TaskWaitMs)
		assert.Eq(t, 1, a.ImmediateWaits) // started at 1000ms, waited at 2000ms
		// Only the shell_exec that started it overlaps the task's run.
		assert.Eq(t, int64(10), a.BackgroundOverlapMs)
		assert.Eq(t, 3, a.ToolCalls) // shell_exec, task wait, task output
	})

	t.Run("a wait after another tool call is no immediate wait", func(t *testing.T) {
		a := AnalyzeTrace([]store.Event{
			background(0, 10, "t1"),
			model(20, 100),
			tool("file_read", 200, 1000, nil), // the loop did other work meanwhile
			model(1200, 100),
			tool("task", 1500, 500, map[string]any{"arg": "action=wait id=t1"}),
			task("t1", 0, 3000),
		})
		assert.Eq(t, 1, a.TaskWaits)
		assert.Eq(t, int64(500), a.TaskWaitMs)
		assert.Eq(t, 0, a.ImmediateWaits)
		// 0-10 start, 20-120 and 1200-1300 model, 200-1200 read.
		assert.Eq(t, int64(1210), a.BackgroundOverlapMs)
	})

	t.Run("two tasks started together, then waited for one", func(t *testing.T) {
		a := AnalyzeTrace([]store.Event{
			tool("shell_exec", 0, 10, map[string]any{"arg": "command=go build ./...", "task_id": "t1"}),
			background(20, 10, "t2"),
			model(40, 100),
			tool("task", 200, 100, map[string]any{"arg": "action=wait id=t1"}),
			tool("task", 400, 100, map[string]any{"arg": "action=wait id=t2"}),
			task("t1", 0, 300),
			task("t2", 20, 500),
		})
		assert.Eq(t, 2, a.TaskWaits)
		assert.Eq(t, int64(200), a.TaskWaitMs)
		assert.Eq(t, 2, a.ImmediateWaits)
		assert.Eq(t, int64(230), a.BackgroundOverlapMs) // 0-10, 20-30 and 40-140, once per task
	})

	t.Run("watch and Stop hook tasks are counted by kind", func(t *testing.T) {
		watch := store.Event{Kind: store.EventTask, Name: "t2", Status: eventOK, StartedAt: at(100), DurationMs: 30000,
			Attrs: map[string]any{"kind": "watch", "arg": "go test ./... (every 30s)", "runs": float64(5), "changes": float64(2)}}
		hook := store.Event{Kind: store.EventTask, Name: "t3", Status: eventOK, StartedAt: at(31000), DurationMs: 2100,
			Attrs: map[string]any{"kind": "hook", "arg": "Stop hook"}}

		// A shell task counts in BackgroundTasks, but as neither kind.
		// A model call while the watch runs (1000-1500ms) is no time saved.
		a := AnalyzeTrace([]store.Event{model(0, 100), task("t1", 100, 10), watch, model(1000, 500), hook})
		assert.Eq(t, 3, a.BackgroundTasks)
		assert.Eq(t, 1, a.WatchTasks)
		assert.Eq(t, 5, a.WatchRuns)
		assert.Eq(t, 2, a.WatchChanges)
		assert.Eq(t, 1, a.StopHooks)
		// Their time is not waiting saved: only the shell task's counts.
		assert.Eq(t, int64(10), a.BackgroundMs)
		assert.Eq(t, AnalyzeTrace([]store.Event{model(0, 100), task("t1", 100, 10), model(1000, 500)}).BackgroundOverlapMs, a.BackgroundOverlapMs)

		// The text rows appear only for the kinds the session had.
		var out bytes.Buffer
		assert.NoErr(t, WriteTraceAnalysis(&out, AnalyzeTrace([]store.Event{model(0, 100), task("t1", 0, 300)}), false))
		assert.NotContains(t, out.String(), "watch tasks")
		assert.NotContains(t, out.String(), "stop hooks")

		out.Reset()
		assert.NoErr(t, WriteTraceAnalysis(&out, AnalyzeTrace([]store.Event{watch, hook}), false))
		assert.StrContains(t, out.String(), "watch tasks")
		assert.StrContains(t, out.String(), "1 (5 runs, 2 changes)")
		assert.StrContains(t, out.String(), "1 background Stop hook runs")
	})

	t.Run("the text output adds a row only with background tasks", func(t *testing.T) {
		var out bytes.Buffer
		none := AnalyzeTrace([]store.Event{model(0, 100)})
		assert.NoErr(t, WriteTraceAnalysis(&out, none, false))
		assert.StrContains(t, out.String(), "background tasks")
		assert.NotContains(t, out.String(), "background overlap")

		out.Reset()
		a := AnalyzeTrace([]store.Event{
			model(0, 1000),
			tool("task", 1500, 500, map[string]any{"arg": "action=wait id=t1"}),
			task("t1", 0, 3000),
		})
		assert.NoErr(t, WriteTraceAnalysis(&out, a, false))
		assert.StrContains(t, out.String(), "background overlap     1.0s while model/tool calls ran (waiting saved); 1 waits (0.5s), 0 right after the start")
	})
}

// TestAnalyzeTraceSubAgents: a synchronous sub-agent call brings its steps,
// tool calls, tokens, cost, time and stop with its tool event; a background
// call counts as a call when it starts and brings its steps and tool calls
// with its task's end event (its tokens stay unknown); the text rows appear
// only when sub-agents ran.
func TestAnalyzeTraceSubAgents(t *testing.T) {
	model := func() store.Event {
		return store.Event{Kind: store.EventModel, Status: eventOK, Attrs: map[string]any{}}
	}
	syncCall := store.Event{Kind: store.EventTool, Name: "agent", Status: eventOK, DurationMs: 41300,
		Attrs: map[string]any{"child_session_id": "sess_child", "agent_type": "explore", "steps": float64(12),
			"tool_calls": float64(5), "input_tokens": float64(85000), "cache_read_tokens": float64(60000),
			"output_tokens": float64(3000), "cost_usd": 0.0123, "stop": "final"}}
	start := store.Event{Kind: store.EventTool, Name: "agent", Status: eventOK, DurationMs: 6,
		Attrs: map[string]any{"task_id": "t4", "background": true, "agent_type": "research"}}
	task := store.Event{Kind: store.EventTask, Name: "t4", Status: eventOK, DurationMs: 20000,
		Attrs: map[string]any{"kind": "agent", "arg": "agent: survey deps", "steps": float64(7), "tool_calls": float64(3),
			"input_tokens": float64(15000), "cache_read_tokens": float64(10000), "output_tokens": float64(1000),
			"child_session_id": "sess_bg", "stop": "max_tokens", "cost_usd": "0.0077"}} // labels are text

	a := AnalyzeTrace([]store.Event{model(), syncCall, model(), start, model(), task})
	assert.Eq(t, 2, a.AgentCalls)
	assert.Eq(t, 19, a.AgentSteps)
	assert.Eq(t, 8, a.AgentToolCalls)
	assert.Eq(t, int64(100000), a.AgentInputTokens)
	assert.Eq(t, int64(70000), a.AgentCacheReadTokens)
	assert.Eq(t, int64(4000), a.AgentOutputTokens)
	assert.Eq(t, 0.02, a.AgentCostUSD)
	assert.Eq(t, int64(61306), a.AgentMs) // 41300 + 6 + 20000
	assert.Eq(t, map[string]int{"final": 1, "max_tokens": 1}, a.AgentStops)
	// The agent task is background work the loop did not wait for, like a shell task.
	assert.Eq(t, 1, a.BackgroundTasks)
	assert.Eq(t, int64(20000), a.BackgroundMs)

	var out bytes.Buffer
	assert.NoErr(t, WriteTraceAnalysis(&out, a, false))
	assert.StrContains(t, out.String(), "2 calls, 19 steps, 8 tool calls, in 100k (cache 70k), out 4k, cost $0.0200, ran 61.3s")
	assert.StrContains(t, out.String(), "final x1")
	assert.StrContains(t, out.String(), "max_tokens x1")

	out.Reset()
	assert.NoErr(t, WriteTraceAnalysis(&out, AnalyzeTrace([]store.Event{model()}), false))
	assert.NotContains(t, out.String(), "sub-agent")
}

func TestAnalyzeTraceCacheAttribution(t *testing.T) {
	model := func(in, cached int64, reason string, reused, rebuilt, fresh int) store.Event {
		return store.Event{Kind: store.EventModel, Status: eventOK, InputTokens: in, CacheReadTokens: cached,
			Attrs: map[string]any{"prefix": reason, "prefix_reused": float64(reused),
				"prefix_rebuilt": float64(rebuilt), "prefix_fresh": float64(fresh)}}
	}
	a := AnalyzeTrace([]store.Event{
		// 1 token per 4 chars throughout.
		model(1000, 0, prefixFirst, 0, 0, 4000),                    // all fresh
		model(1200, 1000, prefixAppend, 4000, 0, 800),              // 200 fresh
		model(1300, 600, prefixFold, 2400, 2000, 800),              // 500 rebuilt, 200 fresh
		model(1400, 900, prefixAppend, 5200, 0, 400),               // 100 fresh, 400 the provider missed
		{Kind: store.EventModel, Status: eventOK, InputTokens: 50}, // no attrs (older session)
	})
	assert.Eq(t, int64(1000+200+700+500+50), a.Uncached)
	assert.Eq(t, int64(1000+200+200+100), a.UncachedFresh)
	assert.Eq(t, map[string]int64{prefixFold: 500}, a.UncachedRebuilt)
	assert.Eq(t, int64(400), a.UncachedProvider)
	assert.Eq(t, map[string]int{prefixFold: 1}, a.PrefixBreaks)

	var out bytes.Buffer
	assert.NoErr(t, WriteTraceAnalysis(&out, a, false))
	assert.StrContains(t, out.String(), "uncached input         2.5k ≈ fresh 1.5k + rebuilt 500 (fold 500 x1) + provider-side 400")

	// The provider served more than the prefix promised: the shares shrink.
	a = AnalyzeTrace([]store.Event{model(1000, 900, prefixFold, 2000, 1000, 1000)})
	assert.Eq(t, int64(100), a.UncachedFresh+a.UncachedRebuilt[prefixFold])
	assert.Eq(t, int64(0), a.UncachedProvider)
}

// TestAnalyzeTraceTestOutcome: a test run whose output reports a failure is
// no pass even when the command exited 0 (a pipe or a later command), and
// a step with several changes is no single-change step.
func TestAnalyzeTraceTestOutcome(t *testing.T) {
	model := store.Event{Kind: store.EventModel, Status: eventOK, Attrs: map[string]any{}}
	shell := func(code int, failed bool) store.Event {
		return store.Event{Kind: store.EventTool, Name: "shell_exec", Status: eventOK,
			Attrs: map[string]any{"arg": "command=go test ./... | tail -5", "exit_code": float64(code), "test_failed": failed}}
	}
	edit := func(changes int) store.Event {
		return store.Event{Kind: store.EventTool, Name: "file_edit", Status: eventOK,
			Attrs: map[string]any{"target": []any{"a.go"}, "changes": float64(changes)}}
	}
	a := AnalyzeTrace([]store.Event{
		model, edit(1),
		model, shell(0, true), // exit 0, but the output reports a failure
		model, edit(3),
		model, edit(1), edit(1),
		model, shell(0, false),
		model,
	})
	assert.Eq(t, 0, a.ExploreSteps)
	assert.Eq(t, 5, a.WriteToGreenSteps)
	assert.Eq(t, 1, a.AfterGreenSteps)
	assert.Eq(t, 3, a.EditSteps)
	assert.Eq(t, 1, a.SingleEditSteps)
	assert.Eq(t, 0, a.TestRunsAfterPass)

	// A focused run (-run) of a scratch test is no green.
	focusedRun := shell(0, false)
	focusedRun.Attrs = map[string]any{"arg": "command=go test -count=1 -run TestScratch -v ./", "exit_code": float64(0), "test_failed": false}
	scratch := AnalyzeTrace([]store.Event{model, edit(1), model, focusedRun, model, edit(1), model, shell(0, false), model})
	assert.Eq(t, 4, scratch.WriteToGreenSteps)
	assert.Eq(t, 1, scratch.AfterGreenSteps)

	// A passing baseline run before the first write is no first pass.
	baseline := AnalyzeTrace([]store.Event{model, shell(0, false), model, edit(1), model, shell(0, false), model, shell(0, false)})
	assert.Eq(t, 1, baseline.TestRunsAfterPass)
	assert.Eq(t, 2, baseline.WriteToGreenSteps)

	none := AnalyzeTrace([]store.Event{model, edit(1), model})
	assert.Eq(t, -1, none.WriteToGreenSteps)
	assert.Eq(t, -1, none.AfterGreenSteps)
}

func TestCallAttrsShellAndEdit(t *testing.T) {
	ws := `D:\work\x\ws`
	attrs := map[string]any{}
	addCallAttrs(attrs, agentcore.ToolCall{Name: "shell_exec", Arguments: map[string]any{
		"command": `cd D:\Work\x\ws; go vet ./...; go test ./... | tail -3`}},
		&agentcore.ToolResult{Content: "--- FAIL: TestA (0.00s)\nFAIL\n[stderr]\nvet: x.go:1:2: bad\n[exit code: 0]"}, ws)
	assert.Eq(t, "command=cd <ws>; go vet ./...; go test ./... | tail -3", attrs["arg"])
	assert.Eq(t, true, attrs["stderr"])
	assert.Eq(t, true, attrs["test_failed"])
	assert.Eq(t, 0, attrs["exit_code"])

	attrs = map[string]any{}
	addCallAttrs(attrs, agentcore.ToolCall{Name: "shell_exec", Arguments: map[string]any{"command": "go test ./..."}},
		&agentcore.ToolResult{Content: "ok  \tx\t0.1s\n[exit code: 0]"}, ws)
	assert.Eq(t, false, attrs["test_failed"])
	assert.Nil(t, attrs["stderr"])

	attrs = map[string]any{}
	addCallAttrs(attrs, agentcore.ToolCall{Name: "file_edit", Arguments: map[string]any{"path": "a.go",
		"edits": []any{map[string]any{"old_string": "a\nb\n", "new_string": "a\nb\nc\n"}, map[string]any{"old_string": "x", "new_string": ""}}}}, nil, ws)
	assert.Eq(t, 2, attrs["changes"])
	assert.Eq(t, 3, attrs["lines_added"])
	assert.Eq(t, 3, attrs["lines_removed"])

	attrs = map[string]any{}
	addCallAttrs(attrs, agentcore.ToolCall{Name: "file_write", Arguments: map[string]any{"path": "b.go", "content": "package b\n\nfunc B() {}\n"}}, nil, ws)
	assert.Eq(t, 3, attrs["lines_added"])
}

// TestCallAttrsTask: a task call records its action and id as attrs, so the
// analysis does not depend on parsing the argument summary (a grep pattern
// with spaces or "id=" in it would mislead the parse).
func TestCallAttrsTask(t *testing.T) {
	attrs := map[string]any{}
	addCallAttrs(attrs, agentcore.ToolCall{Name: "task", Arguments: map[string]any{
		"action": "output", "id": "t2", "grep": "FAIL id=t9 x"}}, nil, "")
	assert.Eq(t, "output", attrs["action"])
	assert.Eq(t, "t2", attrs["id"])
	action, id := taskAction(store.Event{Attrs: attrs})
	assert.Eq(t, "output", action)
	assert.Eq(t, "t2", id)
}

func TestContextAndFinishTraceLines(t *testing.T) {
	line := traceLine(store.Event{Kind: store.EventContext, Name: "turn", Status: eventOK, Attrs: map[string]any{
		"system": float64(3000), "tools": float64(12000), "tools_count": float64(14), "tools_mcp": float64(2),
		"rules": float64(1500), "history": float64(0), "input": float64(80), "est_tokens": float64(4500), "window": float64(1000000)}})
	assert.StrContains(t, line, "chars system=3k tools=12k(14, 2 mcp) rules=1.5k input=80 est=4.5k/1M tokens")
	tool := traceLine(store.Event{Kind: store.EventTool, Name: "shell_exec", Status: eventOK, Attrs: map[string]any{"approval": "user", "approval_wait_ms": float64(3200)}})
	assert.StrContains(t, tool, "approval=user(wait=3.2s)")
	assert.NotContains(t, traceLine(store.Event{Kind: store.EventTool, Name: "file_read", Status: eventOK, Attrs: map[string]any{"approval": "rule"}}), "approval")
	model := traceLine(store.Event{Kind: store.EventModel, Name: "m", Status: eventOK, Attrs: map[string]any{"finish": "truncated"}})
	assert.StrContains(t, model, " truncated")
	assert.NotContains(t, traceLine(store.Event{Kind: store.EventModel, Name: "m", Status: eventOK, Attrs: map[string]any{"finish": "final"}}), "final")
}
