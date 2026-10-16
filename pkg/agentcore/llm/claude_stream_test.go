package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	agentcoreapi "github.com/inhere/suga/pkg/agentcore/api"
)

// claudeServer serves body with the given status/content type and records the
// request path, headers and JSON body.
func claudeServer(t *testing.T, status int, ctype, body string, gotPath *string, gotBody *map[string]any, gotHdr *http.Header) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotPath != nil {
			*gotPath = r.URL.Path
		}
		if gotHdr != nil {
			*gotHdr = r.Header.Clone()
		}
		if gotBody != nil {
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, gotBody)
		}
		w.Header().Set("Content-Type", ctype)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

func sseLines(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

func TestClaudeProviderStreamSSE(t *testing.T) {
	req := &agentcoreapi.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "hi"}}}

	t.Run("text and usage", func(t *testing.T) {
		var body map[string]any
		var hdr http.Header
		server := claudeServer(t, 200, "text/event-stream", sseLines(
			`event: message_start`,
			`data: {"type":"message_start","message":{"usage":{"input_tokens":10,"cache_read_input_tokens":4,"cache_creation_input_tokens":2,"output_tokens":1}}}`,
			``,
			`event: ping`,
			`data: {"type":"ping"}`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}}`,
			`data: {"type":"content_block_stop","index":0}`,
			`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
			`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Hel"}}`,
			`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"lo"}}`,
			`data: {"type":"content_block_stop","index":1}`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
			`data: {"type":"message_stop"}`,
		), nil, &body, &hdr)
		var events []agentcoreapi.StreamEvent
		res, err := NewClaudeProvider(ClaudeConfig{APIKey: "k", BaseURL: server.URL}).Stream(context.Background(), req, collect(&events))
		assert.NoErr(t, err)
		assert.Eq(t, "Hello", res.Content)
		assert.Eq(t, "hmm", res.Reasoning)
		assert.Eq(t, agentcoreapi.FinishFinal, res.FinishReason)
		assert.Eq(t, 16, res.Usage.InputTokens)
		assert.Eq(t, 7, res.Usage.OutputTokens)
		assert.Eq(t, 23, res.Usage.TotalTokens)
		assert.Eq(t, 4, res.Usage.CacheReadTokens)
		assert.Eq(t, 2, res.Usage.CacheWriteTokens)
		// Two text deltas then message_stop; thinking is not emitted.
		assert.Eq(t, 3, len(events))
		assert.Eq(t, "Hel", events[0].Delta)
		assert.Eq(t, "lo", events[1].Delta)
		assert.Eq(t, agentcoreapi.StreamEventMessageStop, events[2].Kind)
		// The request really asked for a stream (Stream does not call Generate).
		assert.Eq(t, true, body["stream"])
		assert.Eq(t, "text/event-stream", hdr.Get("Accept"))
		assert.Eq(t, "k", hdr.Get("x-api-key"))
	})

	t.Run("tool_use fragments sorted by index", func(t *testing.T) {
		server := claudeServer(t, 200, "text/event-stream", sseLines(
			`data: {"type":"message_start","message":{"usage":{"input_tokens":3}}}`,
			`data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"t2","name":"noargs"}}`,
			`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"t1","name":"read"}}`,
			`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`,
			`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"a.go\"}"}}`,
			`data: {"type":"content_block_stop","index":1}`,
			`data: {"type":"content_block_stop","index":2}`,
			`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`,
			`data: {"type":"message_stop"}`,
		), nil, nil, nil)
		var events []agentcoreapi.StreamEvent
		res, err := NewClaudeProvider(ClaudeConfig{BaseURL: server.URL}).Stream(context.Background(), req, collect(&events))
		assert.NoErr(t, err)
		assert.Eq(t, agentcoreapi.FinishToolCalls, res.FinishReason)
		assert.Eq(t, 2, len(res.ToolCalls))
		assert.Eq(t, "t1", res.ToolCalls[0].CallID)
		assert.Eq(t, "read", res.ToolCalls[0].Name)
		assert.Eq(t, "a.go", res.ToolCalls[0].Arguments["path"])
		assert.Eq(t, "t2", res.ToolCalls[1].CallID)
		assert.Eq(t, 0, len(res.ToolCalls[1].Arguments))
		assert.Eq(t, 0, len(events)) // no text, so no message_stop either
	})

	t.Run("error event in stream", func(t *testing.T) {
		// A permanent API error (invalid_request_error, no overload wording): the
		// turn fails at once instead of being requested again.
		server := claudeServer(t, 200, "text/event-stream", sseLines(
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"x"}}`,
			`event: error`,
			`data: {"type":"error","error":{"type":"invalid_request_error","message":"bad model"}}`,
		), nil, nil, nil)
		_, err := NewClaudeProvider(ClaudeConfig{BaseURL: server.URL}).Stream(context.Background(), req, collect(&[]agentcoreapi.StreamEvent{}))
		assert.Err(t, err)
		assert.StrContains(t, err.Error(), "claude api error: bad model")
		assert.Eq(t, ErrorKindBadResponse, ErrorKind(err))
	})

	t.Run("4xx is not retried", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls++
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"bad model"}}`)
		}))
		defer server.Close()
		_, err := NewClaudeProvider(ClaudeConfig{BaseURL: server.URL}).Stream(context.Background(), req, nil)
		assert.Err(t, err)
		assert.Eq(t, "claude api error: bad model", err.Error())
		assert.Eq(t, 1, calls)
	})

	t.Run("non-SSE response falls back", func(t *testing.T) {
		server := claudeServer(t, 200, "application/json",
			`{"content":[{"type":"text","text":"plain"}],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":3}}`, nil, nil, nil)
		var events []agentcoreapi.StreamEvent
		res, err := NewClaudeProvider(ClaudeConfig{BaseURL: server.URL}).Stream(context.Background(), req, collect(&events))
		assert.NoErr(t, err)
		assert.Eq(t, "plain", res.Content)
		assert.Eq(t, 5, res.Usage.TotalTokens)
		assert.Eq(t, 2, len(events))
		assert.Eq(t, "plain", events[0].Delta)
		assert.Eq(t, agentcoreapi.StreamEventMessageStop, events[1].Kind)
	})
}

func TestClaudeProviderBaseURL(t *testing.T) {
	req := &agentcoreapi.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "hi"}}}
	for name, suffix := range map[string]string{
		"without v1":        "/gw",
		"with v1":           "/gw/v1",
		"with v1 and slash": "/gw/v1/",
	} {
		t.Run(name, func(t *testing.T) {
			var path string
			server := claudeServer(t, 200, "application/json",
				`{"content":[{"type":"text","text":"ok"}],"usage":{}}`, &path, nil, nil)
			p := NewClaudeProvider(ClaudeConfig{BaseURL: server.URL + suffix})

			_, err := p.Generate(context.Background(), req)
			assert.NoErr(t, err)
			want := "/gw/v1/messages"
			assert.Eq(t, want, path)

			path = ""
			_, err = p.Stream(context.Background(), req, nil)
			assert.NoErr(t, err)
			assert.Eq(t, want, path)
		})
	}

	t.Run("default", func(t *testing.T) {
		assert.Eq(t, "https://api.anthropic.com/v1/messages", claudeMessagesURL(""))
	})
}

func TestClaudeProviderName(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		assert.Eq(t, "anthropic", NewClaudeProvider(ClaudeConfig{}).Name())
	})
	t.Run("custom", func(t *testing.T) {
		assert.Eq(t, "my-gw", NewClaudeProvider(ClaudeConfig{Name: "my-gw"}).Name())
	})
}
