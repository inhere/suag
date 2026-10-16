package personal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/tools"
)

func TestToolEventsOrderAndSummary(t *testing.T) {
	ctx := context.Background()
	repo := openObserveRepo(t)
	reg := tools.NewRegistry()
	reg.Register(fnTool{name: "ok_tool", fn: func() (*agentcore.ToolResult, error) { return &agentcore.ToolResult{Content: "fine"}, nil }})
	reg.Register(fnTool{name: "bad_tool", fn: func() (*agentcore.ToolResult, error) {
		return &agentcore.ToolResult{Content: "boom", IsError: true}, nil
	}})
	long := strings.Repeat("x", 500)
	prov := &reqProv{steps: []*agentcore.GenerateResult{
		callStep("c1", "ok_tool", map[string]any{"path": "a.go", "token": "secret=abc123"}),
		callStep("c2", "bad_tool", map[string]any{"data": long}),
		{Content: "done", FinishReason: agentcore.FinishFinal},
	}}
	runner, err := New(Config{Repository: repo, Provider: prov, Tools: reg})
	assert.NoErr(t, err)

	var events []Event
	_, err = runner.Run(ctx, RunRequest{Prompt: "go", Emit: func(_ context.Context, e Event) error {
		if e.Kind == "tool_call" || e.Kind == "tool_result" {
			events = append(events, e)
		}
		return nil
	}})
	assert.NoErr(t, err)
	if !assert.Eq(t, 4, len(events)) {
		return
	}

	t.Run("order", func(t *testing.T) {
		kinds := []string{events[0].Kind, events[1].Kind, events[2].Kind, events[3].Kind}
		assert.Eq(t, []string{"tool_call", "tool_result", "tool_call", "tool_result"}, kinds)
		assert.Eq(t, "ok_tool", events[0].ToolName)
		assert.Eq(t, "bad_tool", events[3].ToolName)
	})
	t.Run("argument summary", func(t *testing.T) {
		assert.StrContains(t, events[0].Text, `"path":"a.go"`)
		assert.StrNotContains(t, events[0].Text, "abc123")
		assert.Eq(t, toolArgSummaryLimit, len([]rune(events[2].Text)))
	})
	t.Run("error flag", func(t *testing.T) {
		assert.False(t, events[1].IsError)
		assert.True(t, events[3].IsError)
		assert.Eq(t, "boom", events[3].Text)
	})
}

// TestToolEventCarriesFileChange: with Config.DiffPreview the "tool_call" event
// of a file tool carries what the call changes on disk (chat previews it);
// without it no file text travels with the events (run, ACP, stream-json).
func TestToolEventCarriesFileChange(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "a.go"), []byte("old := 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(t *testing.T, preview bool) []Event {
		t.Helper()
		repo := openObserveRepo(t)
		reg := tools.NewRegistry()
		reg.Register(fnTool{name: "file_edit", fn: func() (*agentcore.ToolResult, error) {
			return &agentcore.ToolResult{Name: "file_edit", Content: "edited"}, nil
		}})
		prov := &reqProv{steps: []*agentcore.GenerateResult{
			callStep("c1", "file_edit", map[string]any{"path": "a.go", "old_string": "old := 1", "new_string": "old := 2"}),
			{Content: "done", FinishReason: agentcore.FinishFinal},
		}}
		runner, err := New(Config{Repository: repo, Provider: prov, Tools: reg, Workspace: work, DiffPreview: preview})
		assert.NoErr(t, err)
		var events []Event
		_, err = runner.Run(ctx, RunRequest{Prompt: "go", Emit: func(_ context.Context, e Event) error {
			if e.Kind == "tool_call" {
				events = append(events, e)
			}
			return nil
		}})
		assert.NoErr(t, err)
		assert.Require(t, assert.Len(t, events, 1))
		return events
	}

	t.Run("the change is carried when asked for", func(t *testing.T) {
		change := run(t, true)[0].Change
		assert.NotNil(t, change)
		assert.Eq(t, "a.go", change.Rel)
		assert.Eq(t, "old := 1\n", *change.Old)
		assert.Eq(t, "old := 2\n", *change.New)
	})

	t.Run("nothing is carried by default", func(t *testing.T) {
		assert.Nil(t, run(t, false)[0].Change)
	})
}

func TestToolEventEmitErrorDoesNotAbortTurn(t *testing.T) {
	repo := openObserveRepo(t)
	reg := tools.NewRegistry()
	ran := 0
	reg.Register(fnTool{name: "ok_tool", fn: func() (*agentcore.ToolResult, error) { ran++; return &agentcore.ToolResult{Content: "fine"}, nil }})
	prov := &reqProv{steps: []*agentcore.GenerateResult{
		callStep("c1", "ok_tool", nil),
		{Content: "done", FinishReason: agentcore.FinishFinal},
	}}
	runner, err := New(Config{Repository: repo, Provider: prov, Tools: reg})
	assert.NoErr(t, err)
	res, err := runner.Run(context.Background(), RunRequest{Prompt: "go", Emit: func(_ context.Context, e Event) error {
		if e.Kind == "tool_call" || e.Kind == "tool_result" {
			return errors.New("display broke")
		}
		return nil
	}})
	assert.NoErr(t, err)
	assert.Eq(t, 1, ran)
	assert.Eq(t, "done", res.Output)
}
