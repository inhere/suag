package llm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gookit/goutil/x/assert"

	agentcoreapi "github.com/inhere/suga/pkg/agentcore/api"
)

// countingServer answers every request with status and an API error body, so a
// pool test drives the real provider error path. It reports the request count.
func countingServer(t *testing.T, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, `{"error":{"message":"status %d"}}`, status)
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

// jsonServer answers every request with a whole completion. It reports the
// request count.
func jsonServer(t *testing.T, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

// testPoolProvider is a real provider aimed at one test server; retries are off
// so a failure is reported after one request (fastRetry still allows 3, which
// the transient cases exercise).
func testPoolProvider(t *testing.T, name, url string) *OpenAIProvider {
	t.Helper()
	return NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: url, Model: "m", Name: name, Retry: fastRetry})
}

const wholeCompletion = `{"choices":[{"message":{"content":"backup"},"finish_reason":"stop"}]}`

// TestPoolFallsBackByErrorKind: the pool asks the next model only when it still
// has a chance (llm.ShouldFallback) - a transient failure, or a rejection that
// names this provider (401/403/404). A request the provider refused on the
// request's own account (400/422) comes straight back, without spending the
// fallback models on an error they would hit too.
//
// Reverse verification: restoring the old "fall back on every error" loop (this
// test's 400 and 422 cases) makes the second provider be called, so
// assert.Eq(t, int32(0), backupHits.Load()) fails.
func TestPoolFallsBackByErrorKind(t *testing.T) {
	req := &agentcoreapi.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "hi"}}}
	for _, tc := range []struct {
		name         string
		status       int
		wantFallback bool
	}{
		{"400 describes the request", http.StatusBadRequest, false},
		{"422 describes the request", http.StatusUnprocessableEntity, false},
		{"401 names the credentials", http.StatusUnauthorized, true},
		{"403 names the credentials", http.StatusForbidden, true},
		{"404 names the model", http.StatusNotFound, true},
		{"503 is transient", http.StatusServiceUnavailable, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			primary, primaryHits := countingServer(t, tc.status)
			backup, backupHits := jsonServer(t, wholeCompletion)
			pool := NewPool([]Provider{testPoolProvider(t, "primary", primary.URL), testPoolProvider(t, "backup", backup.URL)})

			res, err := pool.Generate(context.Background(), req)
			if !tc.wantFallback {
				assert.Require(t, assert.Err(t, err))
				// The provider's own error, not an "all providers failed" wrap:
				// nothing else was tried.
				assert.StrContains(t, err.Error(), fmt.Sprintf("status %d", tc.status))
				assert.Eq(t, ErrorKindRejected, ErrorKind(err))
				assert.Eq(t, int32(0), backupHits.Load())
				assert.Gt(t, primaryHits.Load(), int32(0))
				return
			}
			assert.Require(t, assert.NoErr(t, err))
			assert.Eq(t, "backup", res.Content)
			assert.Eq(t, int32(1), backupHits.Load())
		})
	}
}

// TestPoolStreamFallsBackByErrorKind: the streaming path follows the same rule.
func TestPoolStreamFallsBackByErrorKind(t *testing.T) {
	req := &agentcoreapi.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "hi"}}}
	const streamBody = "data: {\"choices\":[{\"delta\":{\"content\":\"backup\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"

	t.Run("a 400 is returned", func(t *testing.T) {
		primary, _ := countingServer(t, http.StatusBadRequest)
		backup, backupHits := sseServerCount(t, streamBody)
		pool := NewPool([]Provider{testPoolProvider(t, "primary", primary.URL), testPoolProvider(t, "backup", backup.URL)})

		_, err := pool.Stream(context.Background(), req, nil)
		assert.Require(t, assert.Err(t, err))
		assert.Eq(t, ErrorKindRejected, ErrorKind(err))
		assert.Eq(t, int32(0), backupHits.Load())
	})
	t.Run("a 429 moves on", func(t *testing.T) {
		primary, _ := countingServer(t, http.StatusTooManyRequests)
		backup, backupHits := sseServerCount(t, streamBody)
		pool := NewPool([]Provider{testPoolProvider(t, "primary", primary.URL), testPoolProvider(t, "backup", backup.URL)})

		res, err := pool.Stream(context.Background(), req, nil)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "backup", res.Content)
		assert.Eq(t, int32(1), backupHits.Load())
	})
}

// sseServerCount is sseServer with a request counter, for the streaming pool
// cases (the backup has to stream, see openStream).
func sseServerCount(t *testing.T, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

// TestPoolReportsWhyItFallsBack: the failed attempt's model event says what the
// pool did with the failure (Attempt.Fallback), which is what the trace shows
// and the log line repeats - both for a fallback and for a returned error.
func TestPoolReportsWhyItFallsBack(t *testing.T) {
	req := &agentcoreapi.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "hi"}}}

	t.Run("falling back names the kind", func(t *testing.T) {
		primary, _ := countingServer(t, http.StatusNotFound)
		backup, _ := jsonServer(t, wholeCompletion)
		pool := NewPool([]Provider{testPoolProvider(t, "primary", primary.URL), testPoolProvider(t, "backup", backup.URL)})
		var attempts []Attempt
		pool.SetObserver(func(_ context.Context, a Attempt) { attempts = append(attempts, a) })

		_, err := pool.Generate(context.Background(), req)
		assert.Require(t, assert.NoErr(t, err))
		assert.Require(t, assert.Eq(t, 2, len(attempts)))
		assert.Eq(t, "next, provider_rejected (HTTP 404)", attempts[0].Fallback)
		assert.Eq(t, "", attempts[1].Fallback) // the successful one
	})

	t.Run("returning the error names why not", func(t *testing.T) {
		primary, _ := countingServer(t, http.StatusBadRequest)
		backup, _ := jsonServer(t, wholeCompletion)
		pool := NewPool([]Provider{testPoolProvider(t, "primary", primary.URL), testPoolProvider(t, "backup", backup.URL)})
		var attempts []Attempt
		pool.SetObserver(func(_ context.Context, a Attempt) { attempts = append(attempts, a) })

		_, err := pool.Generate(context.Background(), req)
		assert.Require(t, assert.Err(t, err))
		assert.Require(t, assert.Eq(t, 1, len(attempts)))
		assert.Eq(t, "stop, provider_rejected (HTTP 400) would repeat on the next model", attempts[0].Fallback)
	})
}

// TestPoolQuotaSkipsTheSameProvider: an exhausted allowance belongs to the
// provider's account, so the pool skips the other models of that provider and
// asks the next different provider (issue suga-ok1r). Reverse check: with
// nextProviderIndex returning i+1 the second model of the exhausted provider is
// asked and its hit count is 1.
func TestPoolQuotaSkipsTheSameProvider(t *testing.T) {
	quota := func(t *testing.T) (*httptest.Server, *atomic.Int32) {
		t.Helper()
		var hits atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"You've reached your weekly usage limit for your plan. Your limit resets at 2026-10-19T06:50:37.174Z.","type":"rate_limit_error","code":"RATE_LIMITED"}}`)
		}))
		t.Cleanup(server.Close)
		return server, &hits
	}
	t.Run("another model of the exhausted provider is skipped", func(t *testing.T) {
		first, firstHits := quota(t)
		sibling, siblingHits := quota(t)
		other, otherHits := jsonServer(t, wholeCompletion)
		pool := NewPool([]Provider{
			testPoolProvider(t, "command-code", first.URL),
			testPoolProvider(t, "command-code", sibling.URL),
			testPoolProvider(t, "opencode-go", other.URL),
		})
		res, err := pool.Generate(context.Background(), &agentcoreapi.GenerateRequest{})
		assert.NoErr(t, err)
		assert.Eq(t, "backup", res.Content)
		assert.Eq(t, int32(1), firstHits.Load(), "a quota answer is not retried")
		assert.Eq(t, int32(0), siblingHits.Load(), "the same provider's other model is skipped")
		assert.Eq(t, int32(1), otherHits.Load())
	})
	t.Run("only the same provider left: the quota error comes back", func(t *testing.T) {
		first, _ := quota(t)
		sibling, siblingHits := quota(t)
		pool := NewPool([]Provider{
			testPoolProvider(t, "command-code", first.URL),
			testPoolProvider(t, "command-code", sibling.URL),
		})
		_, err := pool.Generate(context.Background(), &agentcoreapi.GenerateRequest{})
		assert.Err(t, err)
		assert.Eq(t, ErrorKindQuota, ErrorKind(err))
		assert.Eq(t, int32(0), siblingHits.Load())
	})
}
