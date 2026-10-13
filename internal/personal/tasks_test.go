package personal

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/tasks"
	"github.com/inhere/suag/pkg/agentcore/tools"
)

// fakeNotices hands out its infos once they are set.
type fakeNotices struct {
	mu    sync.Mutex
	infos []tasks.Info
}

func (f *fakeNotices) set(infos ...tasks.Info) { f.mu.Lock(); f.infos = infos; f.mu.Unlock() }
func (f *fakeNotices) TakeNotices(string) []tasks.Info {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.infos
	f.infos = nil
	return out
}

// scriptProvider answers a tool call first, then finals; it keeps every
// request's messages.
type scriptProvider struct {
	calls    int
	requests [][]agentcore.Message
}

func (*scriptProvider) Name() string { return "script" }
func (p *scriptProvider) Generate(_ context.Context, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	p.requests = append(p.requests, append([]agentcore.Message(nil), req.Messages...))
	p.calls++
	if p.calls == 1 {
		return &agentcore.GenerateResult{FinishReason: agentcore.FinishToolCalls,
			ToolCalls: []agentcore.ToolCall{{CallID: "c1", Name: "work", Arguments: map[string]any{}}}}, nil
	}
	return &agentcore.GenerateResult{Content: "noted", FinishReason: agentcore.FinishFinal}, nil
}

// indexOf returns the index of the first message containing text, or -1.
func indexOf(msgs []agentcore.Message, text string) int {
	for i, m := range msgs {
		if strings.Contains(m.Content, text) {
			return i
		}
	}
	return -1
}

func TestTaskNoticesReachTheModel(t *testing.T) {
	repo := openObserveRepo(t)
	notices := &fakeNotices{}
	ended := tasks.Info{ID: "t1", Kind: "shell", Summary: "go test ./...", Status: tasks.Exited, ExitCode: 1,
		Started: time.Now().Add(-time.Minute), Ended: time.Now(), Lines: 2, Ref: "out:s:ab", Tail: []string{"--- FAIL: TestX", "FAIL"}}
	registry := tools.NewRegistry()
	registry.Register(fnTool{name: "work", fn: func() (*agentcore.ToolResult, error) {
		notices.set(ended) // the task ends while the tool runs
		return &agentcore.ToolResult{Content: "worked"}, nil
	}})
	provider := &scriptProvider{}
	runner, err := New(Config{Repository: repo, Tools: registry, Provider: provider, Tasks: notices})
	assert.Require(t, assert.NoErr(t, err))

	res, err := runner.Run(context.Background(), RunRequest{Prompt: "go"})
	assert.Require(t, assert.NoErr(t, err))
	assert.Require(t, assert.Len(t, provider.requests, 2))
	assert.Eq(t, -1, indexOf(provider.requests[0], "background task notice"))
	second := provider.requests[1]
	notice := indexOf(second, "[background task notice]")
	assert.Require(t, assert.True(t, notice > 0))
	assert.Eq(t, notice-1, indexOf(second, "worked")) // right after the tool result
	assert.StrContains(t, second[notice].Content, "t1 exited (exit 1) after 1m0s")
	assert.StrContains(t, second[notice].Content, "last lines:\n  --- FAIL: TestX\n  FAIL")

	t.Run("the next turn replays it in place", func(t *testing.T) {
		provider.calls = 1 // straight to the final answer
		_, err := runner.Run(context.Background(), RunRequest{SessionID: res.SessionID, Prompt: "again"})
		assert.Require(t, assert.NoErr(t, err))
		last := provider.requests[len(provider.requests)-1]
		at := indexOf(last, "[background task notice]")
		assert.Require(t, assert.True(t, at > 0))
		assert.Eq(t, at-1, indexOf(last, "worked"))
		assert.True(t, indexOf(last, "noted") > at)
	})
	t.Run("a task end is a trace event", func(t *testing.T) {
		ended.SessionID = res.SessionID
		runner.RecordTaskEnd(ended)
		events := eventsOf(t, repo, res.SessionID, store.EventTask)
		assert.Require(t, assert.Len(t, events, 1))
		assert.Eq(t, "t1", events[0].Name)
		assert.Eq(t, "exited", events[0].Status)
		assert.Eq(t, "exit code 1", events[0].Error)
		assert.Eq(t, "go test ./...", events[0].Attrs["arg"])
		assert.True(t, events[0].DurationMs >= 59000)
	})
}
