package personal

import (
	"bytes"
	"context"
	"testing"

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

	a := AnalyzeTrace(eventsOf(t, repo, res.SessionID))
	assert.Eq(t, 1, a.ShellFailures)
	assert.Eq(t, 1, a.TestRuns)
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
		"edits": []any{map[string]any{}, map[string]any{}}}}, nil, ws)
	assert.Eq(t, 2, attrs["changes"])
}
