package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"

	agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"
)

// brokenStreamServer streams partial and then drops the connection mid-body
// for the first `breaks` requests; later requests get the whole stream. hits
// counts the requests.
func brokenStreamServer(t *testing.T, partial, whole string, breaks int32, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		n := hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if n > breaks {
			_, _ = io.WriteString(w, whole)
			return
		}
		_, _ = io.WriteString(w, partial)
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler) // close the connection without ending the chunked body
	}))
	t.Cleanup(server.Close)
	return server
}

func noStreamRetryDelay(t *testing.T) {
	t.Helper()
	prev := streamRetryDelay
	streamRetryDelay = func(int) time.Duration { return 0 }
	t.Cleanup(func() { streamRetryDelay = prev })
}

func TestStreamRetriesBrokenReads(t *testing.T) {
	noStreamRetryDelay(t)
	req := &agentcoreapi.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "hi"}}}
	sse := func(lines ...string) string { return strings.Join(lines, "\n") + "\n" }
	toolPartial := sse(
		`data: {"choices":[{"delta":{"reasoning_content":"plan"}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"file_read","arguments":"{\"pa"}}]}}]}`,
	)
	toolWhole := sse(
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c2","function":{"name":"file_read","arguments":"{\"path\":\"a.go\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	)
	openai := func(url string) *OpenAIProvider {
		return NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: url, Model: "m"})
	}

	t.Run("broken before any text: requested again", func(t *testing.T) {
		var hits atomic.Int32
		server := brokenStreamServer(t, toolPartial, toolWhole, 1, &hits)
		var events []agentcoreapi.StreamEvent
		res, err := openai(server.URL).Stream(context.Background(), req, collect(&events))
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, int32(2), hits.Load())
		assert.Eq(t, 1, len(res.ToolCalls))
		assert.Eq(t, "c2", res.ToolCalls[0].CallID) // nothing of the broken attempt leaks in
		assert.Eq(t, "a.go", res.ToolCalls[0].Arguments["path"])
		assert.Eq(t, "", res.Reasoning)
		for _, ev := range events {
			assert.NotEq(t, agentcoreapi.StreamEventTextDelta, ev.Kind)
		}
	})
	t.Run("broken after text: not repeated", func(t *testing.T) {
		var hits atomic.Int32
		partial := sse(`data: {"choices":[{"delta":{"content":"Hel"}}]}`)
		server := brokenStreamServer(t, partial, sse(`data: {"choices":[{"delta":{"content":"Hello"}}]}`, `data: [DONE]`), 1, &hits)
		var events []agentcoreapi.StreamEvent
		_, err := openai(server.URL).Stream(context.Background(), req, collect(&events))
		assert.ErrMsgContains(t, err, "read openai stream")
		assert.Eq(t, int32(1), hits.Load())
		assert.Eq(t, 1, len(events)) // only the "Hel" delta, shown once
	})
	t.Run("keeps breaking: gives up after the retries", func(t *testing.T) {
		var hits atomic.Int32
		server := brokenStreamServer(t, toolPartial, toolWhole, 100, &hits)
		_, err := openai(server.URL).Stream(context.Background(), req, nil)
		assert.ErrMsgContains(t, err, "read openai stream")
		assert.Eq(t, int32(1+streamReadRetries), hits.Load())
	})
	t.Run("a bad chunk is not retried", func(t *testing.T) {
		var hits atomic.Int32
		server := brokenStreamServer(t, "", sse(`data: {not json`), 0, &hits)
		_, err := openai(server.URL).Stream(context.Background(), req, nil)
		assert.ErrMsgContains(t, err, "parse openai stream chunk")
		assert.Eq(t, int32(1), hits.Load())
	})
	t.Run("claude: broken before any text", func(t *testing.T) {
		var hits atomic.Int32
		partial := sse(
			`data: {"type":"message_start","message":{"usage":{"input_tokens":10,"output_tokens":1}}}`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}`,
		)
		whole := sse(
			`data: {"type":"message_start","message":{"usage":{"input_tokens":10,"output_tokens":1}}}`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
			`data: {"type":"content_block_stop","index":0}`,
			`data: {"type":"message_stop"}`,
		)
		server := brokenStreamServer(t, partial, whole, 1, &hits)
		var events []agentcoreapi.StreamEvent
		res, err := NewClaudeProvider(ClaudeConfig{APIKey: "k", BaseURL: server.URL}).Stream(context.Background(), req, collect(&events))
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, int32(2), hits.Load())
		assert.Eq(t, "ok", res.Content)
		assert.Eq(t, "ok", events[0].Delta)
	})
}
