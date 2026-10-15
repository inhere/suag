package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"

	agentcoreapi "github.com/inhere/suga/pkg/agentcore/api"
)

// transportOf returns a streamed client's transport: its response header
// timeout lives there, the client itself has no deadline.
func transportOf(t *testing.T, c *http.Client) *http.Transport {
	t.Helper()
	tr, ok := c.Transport.(*http.Transport)
	assert.Require(t, assert.True(t, ok))
	return tr
}

// The two knobs reach both clients of both provider types: RequestTimeout bounds
// the non-streamed client as a whole, ResponseHeaderTimeout the streamed
// client's wait for the response headers.
func TestProviderRequestTimeouts(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		p := NewOpenAIProvider(OpenAIConfig{})
		assert.Eq(t, DefaultRequestTimeout, p.client.Timeout)
		assert.Eq(t, DefaultResponseHeaderTimeout, transportOf(t, p.streamClient).ResponseHeaderTimeout)

		c := NewClaudeProvider(ClaudeConfig{})
		assert.Eq(t, DefaultRequestTimeout, c.client.Timeout)
		assert.Eq(t, DefaultResponseHeaderTimeout, transportOf(t, c.streamClient).ResponseHeaderTimeout)
	})

	t.Run("configured", func(t *testing.T) {
		p := NewOpenAIProvider(OpenAIConfig{RequestTimeout: 5 * time.Second, ResponseHeaderTimeout: 7 * time.Second})
		assert.Eq(t, 5*time.Second, p.client.Timeout)
		assert.Eq(t, 7*time.Second, transportOf(t, p.streamClient).ResponseHeaderTimeout)

		c := NewClaudeProvider(ClaudeConfig{RequestTimeout: 3 * time.Second, ResponseHeaderTimeout: 4 * time.Second})
		assert.Eq(t, 3*time.Second, c.client.Timeout)
		assert.Eq(t, 4*time.Second, transportOf(t, c.streamClient).ResponseHeaderTimeout)
	})

	t.Run("NoTimeout disables both", func(t *testing.T) {
		p := NewOpenAIProvider(OpenAIConfig{RequestTimeout: NoTimeout, ResponseHeaderTimeout: NoTimeout})
		assert.Eq(t, time.Duration(0), p.client.Timeout)
		assert.Eq(t, time.Duration(0), transportOf(t, p.streamClient).ResponseHeaderTimeout)
	})
}

// A request that outlives RequestTimeout fails instead of hanging; the error is
// shaped like a transient failure, which is what the retry policy expects.
func TestRequestTimeoutEndsASlowRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"late"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(server.Close)

	p := NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "m",
		RequestTimeout: 50 * time.Millisecond, Retry: RetryPolicy{MaxAttempts: 1}})
	_, err := p.Generate(context.Background(), &agentcoreapi.GenerateRequest{
		Messages: []agentcoreapi.Message{{Role: "user", Content: "hi"}}})
	assert.Err(t, err)
	assert.ErrMsgContains(t, err, "request failed after 1 attempts")
	assert.ErrIs(t, err, context.DeadlineExceeded)
}
