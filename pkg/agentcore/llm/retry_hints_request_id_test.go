package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"

	agentcoreapi "github.com/inhere/suga/pkg/agentcore/api"
)

func TestRetryHintsUseProviderHeadersAndMessages(t *testing.T) {
	req := &agentcoreapi.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "hi"}}}
	ok := `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`

	t.Run("Retry-After-Ms takes precedence over Retry-After", func(t *testing.T) {
		var hits atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			if hits.Add(1) == 1 {
				w.Header().Set("Retry-After-Ms", "3")
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = io.WriteString(w, `{"error":{"message":"busy"}}`)
				return
			}
			_, _ = io.WriteString(w, ok)
		}))
		t.Cleanup(server.Close)
		p := NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "m",
			Retry: RetryPolicy{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}})
		ctx, report := withRetryReport(context.Background())
		_, err := p.Generate(ctx, req)
		assert.NoErr(t, err)
		assert.Eq(t, int32(2), hits.Load())
		assert.Eq(t, 3*time.Millisecond, report.Wait)
	})

	for _, tc := range []struct {
		name string
		text string
	}{
		{name: "fractional seconds", text: "try again in 1.5s"},
		{name: "milliseconds", text: "try again in 20ms"},
		{name: "seconds", text: "try again in 2 seconds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if hits.Add(1) == 1 {
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = io.WriteString(w, `{"error":{"message":"`+tc.text+`"}}`)
					return
				}
				_, _ = io.WriteString(w, ok)
			}))
			t.Cleanup(server.Close)
			p := NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "m",
				Retry: RetryPolicy{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}})
			ctx, report := withRetryReport(context.Background())
			_, err := p.Generate(ctx, req)
			assert.NoErr(t, err)
			assert.Eq(t, int32(2), hits.Load())
			// The parsed hint exceeds the configured cap for all three forms
			// except the 20ms hint, which is also capped to 5ms.
			assert.Eq(t, 5*time.Millisecond, report.Wait)
		})
	}
}

// Reverse check: removing the X-Should-Retry guard makes this server receive a second request.
func TestShouldRetryFalseStopsHTTPRetry(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		hits.Add(1)
		w.Header().Set("X-Should-Retry", "false")
		w.Header().Set("x-request-id", "req-no-retry")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"message":"busy"}}`)
	}))
	t.Cleanup(server.Close)
	p := NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "m",
		Retry: RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}})
	_, err := p.Generate(context.Background(), &agentcoreapi.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "hi"}}})
	assert.Err(t, err)
	assert.Eq(t, int32(1), hits.Load())
	assert.Eq(t, ErrorKindRejected, ErrorKind(err))
	assert.ErrMsgContains(t, err, "request-id: req-no-retry")
}

func TestRequestIDIsReportedOnSuccessAndRetryExhaustion(t *testing.T) {
	req := &agentcoreapi.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "hi"}}}
	for _, header := range []string{"x-request-id", "request-id", "anthropic-request-id", "openai-request-id"} {
		t.Run(header, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set(header, "req-success")
				_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
			}))
			t.Cleanup(server.Close)
			var got Attempt
			_, err := GenerateObserved(context.Background(), NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "m"}), req, 1,
				func(_ context.Context, attempt Attempt) { got = attempt })
			assert.NoErr(t, err)
			assert.Eq(t, "req-success", got.RequestID)
		})
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("request-id", "req-failed")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":{"message":"busy"}}`)
	}))
	t.Cleanup(server.Close)
	p := NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "m",
		Retry: RetryPolicy{MaxAttempts: 1, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}})
	var got Attempt
	_, err := GenerateObserved(context.Background(), p, req, 1, func(_ context.Context, attempt Attempt) { got = attempt })
	assert.Err(t, err)
	assert.ErrMsgContains(t, err, "request-id: req-failed")
	assert.Eq(t, "req-failed", got.RequestID)
}
