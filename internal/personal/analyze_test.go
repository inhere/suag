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
	assert.Eq(t, int64(9000), a.Slowest[0].DurationMs)

	var out bytes.Buffer
	assert.NoErr(t, WriteTraceAnalysis(&out, a, false))
	for _, want := range []string{
		"8 model calls, 8 tool calls, 1 parallel steps (max 2 calls in one step)",
		"re-reads               a.go x2",
		"reads after own write  1",
		"tests                  3 runs, 1 after the first pass",
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
	assert.StrContains(t, traceLine(events[0]), "exit=1")

	a := AnalyzeTrace(eventsOf(t, repo, res.SessionID))
	assert.Eq(t, 1, a.ShellFailures)
	assert.Eq(t, 1, a.TestRuns)
}
