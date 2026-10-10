package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"
)

func sseServer(t *testing.T, status int, body string, gotBody *map[string]any) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotBody != nil {
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, gotBody)
		}
		if status != http.StatusOK {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
		} else {
			w.Header().Set("Content-Type", "text/event-stream")
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

func collect(events *[]agentcoreapi.StreamEvent) agentcoreapi.StreamFunc {
	return func(_ context.Context, ev agentcoreapi.StreamEvent) error {
		*events = append(*events, ev)
		return nil
	}
}

func TestOpenAIProviderStreamSSE(t *testing.T) {
	req := &agentcoreapi.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "hi"}}}

	t.Run("text, reasoning and usage", func(t *testing.T) {
		var body map[string]any
		server := sseServer(t, http.StatusOK, strings.Join([]string{
			`: keep-alive`,
			`data: {"choices":[{"delta":{"reasoning_content":"thi"}}]}`,
			`data: {"choices":[{"delta":{"reasoning_content":"nk"}}]}`,
			`data: {"choices":[{"delta":{"content":"Hel"}}]}`,
			``,
			`data: {"choices":[{"delta":{"content":"lo"},"finish_reason":"stop"}]}`,
			`data: {"choices":[],"usage":{"prompt_tokens":9,"completion_tokens":3,"total_tokens":12,"prompt_tokens_details":{"cached_tokens":4}}}`,
			`data: [DONE]`, ``,
		}, "\n"), &body)
		var events []agentcoreapi.StreamEvent
		res, err := NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "m"}).Stream(context.Background(), req, collect(&events))
		assert.NoErr(t, err)
		assert.Eq(t, "Hello", res.Content)
		assert.Eq(t, "think", res.Reasoning)
		assert.Eq(t, agentcoreapi.FinishFinal, res.FinishReason)
		assert.Eq(t, agentcoreapi.Usage{InputTokens: 9, OutputTokens: 3, TotalTokens: 12, CacheReadTokens: 4}, res.Usage)
		assert.Eq(t, 3, len(events))
		assert.Eq(t, "Hel", events[0].Delta)
		assert.Eq(t, "lo", events[1].Delta)
		assert.Eq(t, agentcoreapi.StreamEventMessageStop, events[2].Kind)
		assert.Eq(t, true, body["stream"])
		assert.Eq(t, map[string]any{"include_usage": true}, body["stream_options"])
	})
	t.Run("fragmented tool calls", func(t *testing.T) {
		server := sseServer(t, http.StatusOK, strings.Join([]string{
			`data: {"choices":[{"delta":{"content":"checking"}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"file_read","arguments":"{\"pa"}}]}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"c2","function":{"name":"file_list","arguments":""}}]}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"a.go\"}"}}]}}]}`,
			`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`data: [DONE]`,
		}, "\n"), nil)
		res, err := NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "m"}).Stream(context.Background(), req, nil)
		assert.NoErr(t, err)
		assert.Eq(t, "checking", res.Content)
		assert.Eq(t, agentcoreapi.FinishToolCalls, res.FinishReason)
		assert.Eq(t, 2, len(res.ToolCalls))
		assert.Eq(t, agentcoreapi.ToolCall{CallID: "c1", Name: "file_read", Arguments: map[string]any{"path": "a.go"}}, res.ToolCalls[0])
		assert.Eq(t, "c2", res.ToolCalls[1].CallID)
		assert.Eq(t, map[string]any{}, res.ToolCalls[1].Arguments)
	})
	t.Run("api error", func(t *testing.T) {
		server := sseServer(t, http.StatusBadRequest, `{"error":{"message":"bad model"}}`, nil)
		_, err := NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "m"}).Stream(context.Background(), req, nil)
		assert.Eq(t, "openai api error: bad model", fmt.Sprint(err))
	})
	t.Run("error event mid-stream", func(t *testing.T) {
		server := sseServer(t, http.StatusOK, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\ndata: {\"error\":{\"message\":\"overloaded\"}}\n", nil)
		_, err := NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "m"}).Stream(context.Background(), req, nil)
		assert.Eq(t, "openai api error: overloaded", fmt.Sprint(err))
	})
}

// failingStream emits one delta and then fails, like a dropped connection.
type failingStream struct{ name string }

func (f failingStream) Name() string { return f.name }
func (f failingStream) Generate(context.Context, *agentcoreapi.GenerateRequest) (*agentcoreapi.GenerateResult, error) {
	return nil, errors.New("unused")
}
func (f failingStream) Stream(ctx context.Context, _ *agentcoreapi.GenerateRequest, emit agentcoreapi.StreamFunc) (*agentcoreapi.GenerateResult, error) {
	if err := emit(ctx, agentcoreapi.StreamEvent{Kind: agentcoreapi.StreamEventTextDelta, Delta: "partial"}); err != nil {
		return nil, err
	}
	return nil, errors.New("connection reset")
}

// After text reached the caller, a failure is final: falling back would
// repeat or mix the answer.
func TestPoolStreamDoesNotFallBackAfterOutput(t *testing.T) {
	var events []agentcoreapi.StreamEvent
	pool := NewPool([]Provider{failingStream{name: "a"}, failingStream{name: "b"}})
	_, err := pool.Stream(context.Background(), &agentcoreapi.GenerateRequest{}, collect(&events))
	assert.Eq(t, "connection reset", fmt.Sprint(err))
	assert.Eq(t, 1, len(events))
}
