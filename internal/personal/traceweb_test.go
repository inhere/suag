package personal

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/mock"
	"github.com/inhere/suag/pkg/agentcore/tools"
)

// traceRun runs a turn with a reply text, a tool call and a final answer.
func traceRun(t *testing.T) (*store.Store, string) {
	repo := openObserveRepo(t)
	registry := tools.NewRegistry()
	registry.Register(fnTool{name: "file_read", fn: func() (*agentcore.ToolResult, error) {
		return &agentcore.ToolResult{Content: "1\tpackage a"}, nil
	}})
	runner, err := New(Config{Repository: repo, Tools: registry, Provider: mock.NewProvider(
		mock.ToolCall("c1", "file_read", map[string]any{"path": "a.go"}),
		mock.Final("done"),
	)})
	assert.Require(t, assert.NoErr(t, err))
	res, err := runner.Run(context.Background(), RunRequest{Prompt: "read a.go"})
	assert.Require(t, assert.NoErr(t, err))
	return repo, res.SessionID
}

func TestBuildTraceView(t *testing.T) {
	repo, sid := traceRun(t)
	view, err := BuildTraceView(context.Background(), repo, sid)
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, sid, view.Session.ID)
	assert.Eq(t, 2, view.Analysis.ModelCalls)
	assert.Eq(t, "read a.go", view.Session.Title)

	t.Run("an old whole-prompt title shows as its task line", func(t *testing.T) {
		sess, err := repo.GetSession(context.Background(), sid)
		assert.Require(t, assert.NoErr(t, err))
		sess.Title = goferPrompt
		assert.NoErr(t, repo.UpdateSession(context.Background(), sess))
		v, err := BuildTraceView(context.Background(), repo, sid)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "修复 sessions 列表的排序", v.Session.Title)
	})

	entry := map[int64]traceEntry{}
	for _, en := range view.Entries {
		entry[en.Seq] = en
	}
	var tool, firstModel *webEvent
	for i := range view.Events {
		e := &view.Events[i]
		if e.Kind == store.EventTool && tool == nil {
			tool = e
		}
		if e.Kind == store.EventModel && firstModel == nil {
			firstModel = e
		}
	}
	assert.Require(t, assert.NotNil(t, tool))
	// the tool event carries its call (arguments) and its result
	var args map[string]any
	var result string
	for _, seq := range tool.Entries {
		if en := entry[seq]; en.Args != nil {
			args = en.Args
		} else if en.Role == "tool" {
			result = en.Content
		}
	}
	assert.Eq(t, map[string]any{"path": "a.go"}, args)
	assert.Eq(t, "1\tpackage a", result)
	// the first model call carries the assistant message that asked for the tool
	assert.Require(t, assert.NotEmpty(t, firstModel.Entries))
	assert.Eq(t, "file_read", entry[firstModel.Entries[0]].Tool)
}

func TestServeTraceView(t *testing.T) {
	repo, sid := traceRun(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	urls := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- ServeTraceView(ctx, "127.0.0.1:0", func(ctx context.Context) (*TraceView, error) {
			return BuildTraceView(ctx, repo, sid)
		}, func(url string) { urls <- url })
	}()
	var url string
	select {
	case url = <-urls:
	case err := <-done:
		t.Fatalf("serve: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("no url")
	}
	get := func(path string) (string, string) {
		resp, err := http.Get(url + path)
		assert.Require(t, assert.NoErr(t, err))
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.Header.Get("Content-Type"), string(body)
	}
	ctype, page := get("")
	assert.StrContains(t, ctype, "text/html")
	assert.StrContains(t, page, "<title>suag trace</title>")
	ctype, body := get("api/session")
	assert.StrContains(t, ctype, "application/json")
	var view TraceView
	assert.NoErr(t, json.Unmarshal([]byte(body), &view))
	assert.Eq(t, sid, view.Session.ID)
	cancel()
	select {
	case err := <-done:
		assert.NoErr(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}

	err := ServeTraceView(context.Background(), "0.0.0.0:0", nil, nil)
	assert.ErrMsgContains(t, err, "loopback address only")
}

func TestCapWebText(t *testing.T) {
	assert.Eq(t, "short", capWebText("short", 10))
	got := capWebText(strings.Repeat("中", 10), 7) // cut inside a rune: back to its start
	assert.True(t, strings.HasPrefix(got, "中中\n[... 24 more bytes"))
}
