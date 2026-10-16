package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gookit/goutil/x/assert"

	agentcoreapi "github.com/inhere/suga/pkg/agentcore/api"
)

// Reverse check: treating context errors as transient would increase hits above one and fail these cases.
func TestContextLengthErrorsArePermanentRejections(t *testing.T) {
	req := &agentcoreapi.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "hi"}}}

	t.Run("openai context_length_exceeded is not retried", func(t *testing.T) {
		var hits atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"code":"context_length_exceeded","message":"maximum context length is 128000 tokens"}}`)
		}))
		t.Cleanup(server.Close)
		p := NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "m"})
		_, err := p.Generate(context.Background(), req)
		assert.Eq(t, int32(1), hits.Load())
		assert.Eq(t, ErrorKindRejected, ErrorKind(err))
		assert.ErrMsgContains(t, err, contextLengthHint)
		assert.True(t, ShouldFallback(err))
	})

	t.Run("anthropic prompt is too long is not retried", func(t *testing.T) {
		var hits atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"prompt is too long: 200000 tokens"}}`)
		}))
		t.Cleanup(server.Close)
		p := NewClaudeProvider(ClaudeConfig{APIKey: "k", BaseURL: server.URL, Model: "m"})
		_, err := p.Generate(context.Background(), req)
		assert.Eq(t, int32(1), hits.Load())
		assert.Eq(t, ErrorKindRejected, ErrorKind(err))
		assert.ErrMsgContains(t, err, contextLengthHint)
		assert.True(t, ShouldFallback(err))
	})

	t.Run("stream error has the same classification", func(t *testing.T) {
		var hits atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"error\":{\"code\":\"context_length_exceeded\",\"message\":\"too long\"}}\n\n")
		}))
		t.Cleanup(server.Close)
		p := NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "m", Retry: fastRetry})
		_, err := p.Stream(context.Background(), req, nil)
		assert.Eq(t, int32(1), hits.Load())
		assert.Eq(t, ErrorKindRejected, ErrorKind(err))
		assert.ErrMsgContains(t, err, contextLengthHint)
		assert.True(t, ShouldFallback(err))
	})
}

func TestContextLengthFallbackOnlyMovesToAnotherProvider(t *testing.T) {
	var sameProviderHits, otherProviderHits atomic.Int32
	contextServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sameProviderHits.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":"context_length_exceeded","message":"too long"}}`)
	}))
	otherServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherProviderHits.Add(1)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(contextServer.Close)
	t.Cleanup(otherServer.Close)
	primary := NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: contextServer.URL, Model: "small", Name: "same"})
	sibling := NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: contextServer.URL, Model: "large", Name: "same"})
	other := NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: otherServer.URL, Model: "other", Name: "other"})
	res, err := NewPool([]Provider{primary, sibling, other}).Generate(context.Background(), &agentcoreapi.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "hi"}}})
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, "ok", res.Content)
	assert.Eq(t, int32(1), sameProviderHits.Load())
	assert.Eq(t, int32(1), otherProviderHits.Load())
}
