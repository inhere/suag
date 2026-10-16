package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gookit/goutil/x/assert"

	agentcoreapi "github.com/inhere/suga/pkg/agentcore/api"
)

// cutStreamServer answers the first `cuts` requests with cut and the later ones
// with whole. The handler returns normally, so the client sees a clean end of
// the body: the only sign of the cut is the missing newline of the last line,
// exactly as when a gateway closes the connection mid-response.
func cutStreamServer(t *testing.T, cut string, cuts int32, whole string, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		if hits.Add(1) <= cuts {
			_, _ = io.WriteString(w, cut)
			return
		}
		_, _ = io.WriteString(w, whole)
	}))
	t.Cleanup(server.Close)
	return server
}

// TestStreamTruncationRetries: a stream cut mid-line (a proxy, gateway or the
// provider closing the connection before the newline) must be retried like a
// broken stream, not parsed (issue suga-zq46: the half line reached
// json.Unmarshal and failed the turn with "unexpected end of JSON input").
//
// Reverse check: without the streamLines.truncated() branch in the readers
// every case here fails with "parse ... stream ...: unexpected end of JSON
// input" and the server is hit once instead of twice.
func TestStreamTruncationRetries(t *testing.T) {
	req := &agentcoreapi.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "hi"}}}
	openai := func(url string) *OpenAIProvider {
		return NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: url, Model: "m", Retry: fastRetry})
	}
	claude := func(url string) *ClaudeProvider {
		return NewClaudeProvider(ClaudeConfig{APIKey: "k", BaseURL: url, Retry: fastRetry})
	}
	responses := func(url string) *ResponsesProvider {
		return NewResponsesProvider(ResponsesConfig{OpenAIConfig: OpenAIConfig{APIKey: "k", BaseURL: url, Model: "m", Retry: fastRetry}})
	}

	t.Run("openai: the cut answer is requested again", func(t *testing.T) {
		var hits atomic.Int32
		// The last line has no newline: the connection died while the chunk was
		// on the wire.
		cut := "data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n" +
			"data: {\"choices\":[{\"delta\":{\"content\":\"lo"
		whole := "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
		server := cutStreamServer(t, cut, 1, whole, &hits)
		var events []agentcoreapi.StreamEvent
		res, err := openai(server.URL).Stream(context.Background(), req, collect(&events))
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, int32(2), hits.Load())
		assert.Eq(t, "Hello", res.Content) // the cut attempt's text is not kept
		assert.Require(t, assert.True(t, len(events) >= 3))
		assert.Eq(t, agentcoreapi.StreamEventTextDelta, events[0].Kind)
		assert.Eq(t, "Hel", events[0].Delta)
		assert.Eq(t, agentcoreapi.StreamEventRetry, events[1].Kind)
		assert.Eq(t, "stream broken", events[1].Delta) // the notice text stays
		assert.Eq(t, true, events[1].Metadata["restart"])
		assert.Eq(t, "Hello", events[2].Delta)
	})
	t.Run("openai: cut every time, the failure names the truncation", func(t *testing.T) {
		var hits atomic.Int32
		server := cutStreamServer(t, "data: {\"choices\":[{\"delta\":{\"content\":\"x", 100, "", &hits)
		_, err := openai(server.URL).Stream(context.Background(), req, nil)
		assert.ErrMsgContains(t, err, "stream truncated")
		assert.ErrMsgContains(t, err, "stream broke 3 times")
		assert.StrNotContains(t, err.Error(), "parse openai stream chunk")
		assert.Eq(t, int32(fastRetry.MaxAttempts), hits.Load())
	})
	t.Run("openai: an end without [DONE] or finish_reason is a cut stream", func(t *testing.T) {
		var hits atomic.Int32
		// Complete lines, but the answer stops without any end marker.
		cut := "data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n"
		whole := "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
		server := cutStreamServer(t, cut, 1, whole, &hits)
		res, err := openai(server.URL).Stream(context.Background(), req, nil)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, int32(2), hits.Load())
		assert.Eq(t, "Hello", res.Content)
	})
	t.Run("openai: a whole line that is not JSON is still not retried", func(t *testing.T) {
		var hits atomic.Int32
		server := cutStreamServer(t, "data: {not json}\n", 100, "", &hits)
		_, err := openai(server.URL).Stream(context.Background(), req, nil)
		assert.ErrMsgContains(t, err, "parse openai stream chunk")
		assert.Eq(t, int32(1), hits.Load())
	})
	t.Run("claude: the cut answer is requested again", func(t *testing.T) {
		var hits atomic.Int32
		cut := "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hel"
		whole := strings.Join([]string{
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
			`data: {"type":"message_stop"}`,
		}, "\n") + "\n"
		server := cutStreamServer(t, cut, 1, whole, &hits)
		var events []agentcoreapi.StreamEvent
		res, err := claude(server.URL).Stream(context.Background(), req, collect(&events))
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, int32(2), hits.Load())
		assert.Eq(t, "Hello", res.Content)
		assert.Eq(t, agentcoreapi.StreamEventRetry, events[0].Kind)
		assert.Eq(t, "Hello", events[1].Delta)
	})
	t.Run("claude: an end without message_stop is a cut stream", func(t *testing.T) {
		var hits atomic.Int32
		cut := "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hel\"}}\n"
		whole := strings.Join([]string{
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
			`data: {"type":"message_stop"}`,
		}, "\n") + "\n"
		server := cutStreamServer(t, cut, 1, whole, &hits)
		res, err := claude(server.URL).Stream(context.Background(), req, nil)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, int32(2), hits.Load())
		assert.Eq(t, "Hello", res.Content)
	})
	t.Run("claude: cut every time, the failure names the truncation", func(t *testing.T) {
		var hits atomic.Int32
		server := cutStreamServer(t, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"x`, 100, "", &hits)
		_, err := claude(server.URL).Stream(context.Background(), req, nil)
		assert.ErrMsgContains(t, err, "stream truncated")
		assert.ErrMsgContains(t, err, "stream broke 3 times")
		assert.Eq(t, int32(fastRetry.MaxAttempts), hits.Load())
	})
	t.Run("claude: a bad event line is not retried", func(t *testing.T) {
		var hits atomic.Int32
		// A message_stop that closes the stream properly, after a broken line.
		cut := "data: {not json}\n" + "data: {\"type\":\"message_stop\"}\n"
		server := cutStreamServer(t, cut, 100, "", &hits)
		_, err := claude(server.URL).Stream(context.Background(), req, nil)
		assert.ErrMsgContains(t, err, "parse claude stream event")
		assert.Eq(t, int32(1), hits.Load())
	})
	t.Run("responses: a cut stream with unfinished items is requested again", func(t *testing.T) {
		var hits atomic.Int32
		// Item 1 was started but never finished: the reply is incomplete, so the
		// finished-items fallback must not take it.
		cut := strings.Join([]string{
			`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message"}}`,
			`data: {"type":"response.output_text.delta","output_index":0,"delta":"ok"}`,
			`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function`,
		}, "\n")
		whole := strings.Join([]string{
			`data: {"type":"response.output_text.delta","output_index":0,"delta":"done"}`,
			`data: {"type":"response.completed","response":{"status":"completed","output":[` +
				`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}]}}`,
		}, "\n") + "\n"
		server := cutStreamServer(t, cut, 1, whole, &hits)
		var events []agentcoreapi.StreamEvent
		res, err := responses(server.URL).Stream(context.Background(), req, collect(&events))
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, int32(2), hits.Load())
		assert.Eq(t, "done", res.Content)
		assert.Eq(t, agentcoreapi.StreamEventRetry, events[1].Kind) // after the cut attempt's "ok"
		assert.Eq(t, true, events[1].Metadata["restart"])
	})
	t.Run("responses: cut every time, the failure names the truncation", func(t *testing.T) {
		var hits atomic.Int32
		cut := strings.Join([]string{
			`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message"}}`,
			`data: {"type":"response.output_text.delta","output_index":0,"delta":"ok"}`,
			`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function`,
		}, "\n")
		server := cutStreamServer(t, cut, 100, "", &hits)
		_, err := responses(server.URL).Stream(context.Background(), req, nil)
		assert.ErrMsgContains(t, err, "stream truncated")
		assert.ErrMsgContains(t, err, "stream broke 3 times")
		assert.Eq(t, int32(fastRetry.MaxAttempts), hits.Load())
	})
	t.Run("responses: a cut stream whose items are all finished is not retried", func(t *testing.T) {
		var hits atomic.Int32
		// Every started item is done: the answer is complete but for the summary
		// event, so the fallback answers instead of restarting (as before).
		cut := strings.Join([]string{
			`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message"}}`,
			`data: {"type":"response.output_text.delta","output_index":0,"delta":"ok"}`,
			`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}}`,
		}, "\n")
		server := cutStreamServer(t, cut, 100, "", &hits)
		res, err := responses(server.URL).Stream(context.Background(), req, nil)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, int32(1), hits.Load())
		assert.Eq(t, "ok", res.Content)
	})
}
