package personal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/llm"
	"github.com/inhere/suga/pkg/agentcore/mock"
	"github.com/inhere/suga/pkg/agentcore/tools"
)

func openObserveRepo(t *testing.T) *store.Store {
	t.Helper()
	repo, err := store.Open(newStorePath(t))
	assert.NoErr(t, err)
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func eventsOf(t *testing.T, repo *store.Store, sessionID string, kinds ...string) []store.Event {
	t.Helper()
	events, err := repo.ListEvents(context.Background(), store.EventFilter{SessionID: sessionID, Kinds: kinds})
	assert.NoErr(t, err)
	return events
}

// fnTool is a native tool whose behavior is a closure.
type fnTool struct {
	name string
	fn   func() (*agentcore.ToolResult, error)
}

func (t fnTool) Name() string        { return t.name }
func (t fnTool) Description() string { return "test tool" }
func (t fnTool) Schema() agentcore.ToolSchema {
	return agentcore.ToolSchema{Name: t.name, InputSchema: map[string]any{"type": "object"}}
}
func (t fnTool) Kind() tools.Kind { return tools.KindNative }
func (t fnTool) Execute(context.Context, agentcore.ToolCall) (*agentcore.ToolResult, error) {
	return t.fn()
}

func TestPoolFallbackRecordsOneModelEventPerAttempt(t *testing.T) {
	repo := openObserveRepo(t)
	// The primary fails transiently (an HTTP 503 with retries off), which is what
	// lets the pool ask the backup: a request-level refusal (400/422) or a bad
	// response would come back instead (llm.ShouldFallback).
	server := statusTestServer(t, http.StatusServiceUnavailable)
	first := llm.NewOpenAIProvider(llm.OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "m",
		Name: "primary", Retry: llm.RetryPolicy{MaxAttempts: 1}})
	second := mock.NewProvider(mock.Final("fallback answer")).WithName("backup")
	runner, err := New(Config{Repository: repo, Provider: llm.NewPool([]llm.Provider{first, second}), Model: "m-1", ProviderName: "pool-cfg"})
	assert.NoErr(t, err)
	res, err := runner.Run(context.Background(), RunRequest{Prompt: "hi"})
	assert.NoErr(t, err)
	assert.Eq(t, "fallback answer", res.Output)

	events := eventsOf(t, repo, res.SessionID, store.EventModel)
	assert.Require(t, assert.Eq(t, 2, len(events)))
	t.Run("failed attempt", func(t *testing.T) {
		assert.Eq(t, "primary", events[0].Name)
		assert.Eq(t, eventError, events[0].Status)
		assert.StrContains(t, events[0].Error, "503")
		assert.Eq(t, float64(1), events[0].Attrs["attempt"])
		// Why the pool moved on, for the trace (llm.Attempt.Fallback).
		assert.Eq(t, "next, provider_transient", events[0].Attrs["fallback"])
	})
	t.Run("successful attempt", func(t *testing.T) {
		assert.Eq(t, "backup", events[1].Name)
		assert.Eq(t, eventOK, events[1].Status)
		assert.Eq(t, float64(2), events[1].Attrs["attempt"])
		assert.Eq(t, "m-1", events[1].Attrs["model"]) // filled from the runner config
		assert.Eq(t, res.TurnID, events[1].TurnID)
		if _, ok := events[1].Attrs["fallback"]; ok {
			t.Fatal("a successful attempt carries no fallback decision")
		}
	})
}

func TestToolEventsRecordErrorAndDenial(t *testing.T) {
	repo := openObserveRepo(t)
	registry := tools.NewRegistry()
	registry.Register(fnTool{name: "broken", fn: func() (*agentcore.ToolResult, error) { return nil, errors.New("disk full") }})
	registry.Register(fnTool{name: "mcp_write", fn: func() (*agentcore.ToolResult, error) {
		return nil, tools.Denied("需要许可：mcp_write，本次运行未授权")
	}})
	registry.Register(fnTool{name: "fine", fn: func() (*agentcore.ToolResult, error) { return &agentcore.ToolResult{Content: "ok"}, nil }})
	runner, err := New(Config{Repository: repo, Tools: registry, Provider: mock.NewProvider(
		mock.ToolCall("c1", "broken", map[string]any{}),
		mock.ToolCall("c2", "mcp_write", map[string]any{}),
		mock.ToolCall("c3", "fine", map[string]any{}),
		mock.Final("done"),
	)})
	assert.NoErr(t, err)
	res, err := runner.Run(context.Background(), RunRequest{Prompt: "go"})
	assert.NoErr(t, err)

	toolEvents := eventsOf(t, repo, res.SessionID, store.EventTool)
	assert.Require(t, assert.Eq(t, 2, len(toolEvents)))
	assert.Eq(t, "broken", toolEvents[0].Name)
	assert.Eq(t, eventError, toolEvents[0].Status)
	assert.Eq(t, true, strings.Contains(toolEvents[0].Error, "disk full"))
	assert.Eq(t, "fine", toolEvents[1].Name)
	assert.Eq(t, eventOK, toolEvents[1].Status)

	denials := eventsOf(t, repo, res.SessionID, store.EventDenial)
	assert.Require(t, assert.Eq(t, 1, len(denials)))
	assert.Eq(t, "mcp_write", denials[0].Name)
	assert.Eq(t, eventDenied, denials[0].Status)

	t.Run("lookup passes through the wrapper", func(t *testing.T) {
		lookup, ok := runner.cfg.Tools.(tools.Lookup)
		assert.Require(t, assert.True(t, ok))
		tool, err := lookup.Get("fine")
		assert.NoErr(t, err)
		assert.Eq(t, tools.KindNative, tools.KindOf(tool))
	})
}

func TestStreamingProviderStaysStreamingAndRecordsTTFT(t *testing.T) {
	repo := openObserveRepo(t)
	delta := agentcore.StreamEvent{Kind: agentcore.StreamEventTextDelta, Delta: "he"}
	inner := mock.NewProvider(mock.Stream(mock.Final("hello"), delta))
	runner, err := New(Config{Repository: repo, Provider: inner})
	assert.NoErr(t, err)
	_, isStreaming := runner.cfg.Provider.(agentcore.StreamingProvider)
	assert.True(t, isStreaming)

	var deltas int
	res, err := runner.Run(context.Background(), RunRequest{Prompt: "hi", Emit: func(_ context.Context, ev Event) error {
		if ev.Kind == string(agentcore.StreamEventTextDelta) {
			deltas++
		}
		return nil
	}})
	assert.NoErr(t, err)
	assert.Eq(t, 1, deltas)
	events := eventsOf(t, repo, res.SessionID, store.EventModel)
	assert.Require(t, assert.Eq(t, 1, len(events)))
	assert.Eq(t, true, events[0].Attrs["streamed"])
	assert.Gt(t, events[0].TTFTMs, 0)

	t.Run("non-streaming inner stays non-streaming", func(t *testing.T) {
		p := (&recorder{}).observeProvider(plainProvider{}, "plain", "m")
		_, ok := p.(agentcore.StreamingProvider)
		assert.False(t, ok)
	})
}

// plainProvider implements Generate only (no Stream).
type plainProvider struct{}

func (plainProvider) Name() string { return "plain" }
func (plainProvider) Generate(context.Context, *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	return &agentcore.GenerateResult{Content: "x", FinishReason: agentcore.FinishFinal}, nil
}

type failingSink struct{ calls int }

func (s *failingSink) AppendEvent(context.Context, store.Event) (store.Event, error) {
	s.calls++
	return store.Event{}, errors.New("events table is gone")
}

func TestEventWriteFailureDoesNotFailTurn(t *testing.T) {
	repo := openObserveRepo(t)
	runner, err := New(Config{Repository: repo, Provider: mock.NewProvider(mock.Final("fine"))})
	assert.NoErr(t, err)
	sink := &failingSink{}
	runner.rec.sink = sink
	res, err := runner.Run(context.Background(), RunRequest{Prompt: "hi"})
	assert.NoErr(t, err)
	assert.Eq(t, RunCompleted, res.State)
	assert.Eq(t, "fine", res.Output)
	assert.Gt(t, sink.calls, 0)
}

func TestTraceTimelineIncludesTurnModelToolAndDenial(t *testing.T) {
	repo := openObserveRepo(t)
	registry := tools.NewRegistry()
	registry.Register(fnTool{name: "broken", fn: func() (*agentcore.ToolResult, error) { return nil, errors.New("disk full") }})
	registry.Register(fnTool{name: "mcp_write", fn: func() (*agentcore.ToolResult, error) { return nil, tools.Denied("需要许可：mcp_write") }})
	runner, err := New(Config{Repository: repo, Tools: registry, Version: "0.14.1",
		SkillsStats: func() SkillsStats { return SkillsStats{Listed: 2, Named: 5, Preloaded: 1, Total: 8} }, Provider: mock.NewProvider(
			mock.ToolCall("c1", "broken", map[string]any{}),
			mock.ToolCall("c2", "mcp_write", map[string]any{}),
			mock.Final("done"),
		)})
	assert.NoErr(t, err)
	res, err := runner.Run(context.Background(), RunRequest{Prompt: "go"})
	assert.NoErr(t, err)
	events, err := repo.ListEvents(context.Background(), store.EventFilter{SessionID: res.SessionID, TurnID: res.TurnID})
	assert.NoErr(t, err)

	var text bytes.Buffer
	assert.NoErr(t, WriteTrace(&text, events, nil, false))
	for _, want := range []string{" turn turn completed ", " model ", " tool broken error ", "err=disk full", " denial mcp_write denied ", "turn=" + res.TurnID,
		" turn_context model=/ mode=unknown tools=2#"} {
		assert.Eq(t, true, strings.Contains(text.String(), want))
	}
	var jsonOut bytes.Buffer
	assert.NoErr(t, WriteTrace(&jsonOut, events, nil, true))
	kinds := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(jsonOut.String()), "\n") {
		var row map[string]any
		assert.NoErr(t, json.Unmarshal([]byte(line), &row))
		kinds[row["kind"].(string)] = true
	}
	assert.Eq(t, map[string]bool{"turn": true, "turn_context": true, "context": true, "model": true, "tool": true, "denial": true}, kinds)

	// The suga version is on the turn and in the session meta.
	sess, err := repo.GetSession(context.Background(), res.SessionID)
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, "0.14.1", sess.Meta["suga_version"])
	assert.StrContains(t, text.String(), " suga=0.14.1 ")

	// The context event says what the turn's first request is made of.
	var ctxEvent *store.Event
	for i := range events {
		if events[i].Kind == store.EventContext {
			ctxEvent = &events[i]
		}
	}
	assert.Require(t, assert.NotNil(t, ctxEvent))
	assert.Eq(t, float64(2), ctxEvent.Attrs["tools_count"])
	assert.Gt(t, ctxEvent.Attrs["tools"].(float64), float64(0))
	assert.Gt(t, ctxEvent.Attrs["input"].(float64), float64(0))
	assert.NotNil(t, ctxEvent.Attrs["environment"])
	assert.NotNil(t, ctxEvent.Attrs["est_tokens"])
	assert.Eq(t, []any{"broken", "mcp_write"}, ctxEvent.Attrs["tool_names"])
	assert.Eq(t, float64(2), ctxEvent.Attrs["skills_listed"])
	assert.Eq(t, float64(5), ctxEvent.Attrs["skills_named"])
	assert.Eq(t, float64(8), ctxEvent.Attrs["skills_total"])
}
