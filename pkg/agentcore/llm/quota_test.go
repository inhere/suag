package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gookit/goutil/x/assert"
	api "github.com/inhere/suga/pkg/agentcore/api"
)

// TestQuotaClassificationAndRetryRule covers the reverse case: changing 429
// back to an unconditional retry makes the weekly-limit hit count exceed one.
func TestQuotaClassificationAndRetryRule(t *testing.T) {
	var hits atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"type":"rate_limit_error","code":"RATE_LIMITED","message":"You've reached your weekly usage limit. Your limit resets at 2026-10-19T06:50:37.174Z"}}`)
	}))
	defer s.Close()
	p := NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: s.URL, Model: "m", Retry: fastRetry})
	_, err := p.Generate(context.Background(), &api.GenerateRequest{Messages: []api.Message{{Role: "user", Content: "hi"}}})
	assert.Require(t, assert.Err(t, err))
	assert.Eq(t, ErrorKindQuota, ErrorKind(err))
	assert.Eq(t, "2026-10-19T06:50:37.174Z", ResetAt(err))
	assert.Eq(t, int32(1), hits.Load())
}

func TestQuotaKinds(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"payment required", `{"error":{"message":"billing"}}`, http.StatusPaymentRequired},
		{"insufficient quota", `{"error":{"code":"insufficient_quota","message":"no"}}`, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer s.Close()
			p := NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: s.URL, Model: "m", Retry: RetryPolicy{MaxAttempts: 1}})
			_, err := p.Generate(context.Background(), &api.GenerateRequest{Messages: []api.Message{{Role: "user", Content: "hi"}}})
			assert.Require(t, assert.Err(t, err))
			assert.Eq(t, ErrorKindQuota, ErrorKind(err))
			assert.True(t, ShouldFallback(err))
		})
	}
}
