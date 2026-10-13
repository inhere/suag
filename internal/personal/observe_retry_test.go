package personal

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/llm"
)

// TestModelEventRecordsRetries: a 503 answered first is retried by the
// provider, announced to Emit as a "retry" event and counted on the model
// event (trace line and analysis).
func TestModelEventRecordsRetries(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if hits.Add(1) == 1 {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	repo := openObserveRepo(t)
	provider := llm.NewOpenAIProvider(llm.OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "m",
		Retry: llm.RetryPolicy{MaxAttempts: 3, BaseDelay: 5 * time.Millisecond, MaxDelay: 5 * time.Millisecond}})
	runner, err := New(Config{Repository: repo, Provider: provider})
	assert.NoErr(t, err)

	var retries []Event
	res, err := runner.Run(context.Background(), RunRequest{Prompt: "hi", Emit: func(_ context.Context, ev Event) error {
		if ev.Kind == string(agentcore.StreamEventRetry) {
			retries = append(retries, ev)
		}
		return nil
	}})
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, "ok", res.Output)
	assert.Require(t, assert.Len(t, retries, 1))
	assert.Eq(t, "HTTP 503", retries[0].Text)
	assert.Require(t, assert.NotNil(t, retries[0].Retry))
	assert.Eq(t, 1, retries[0].Retry.Attempt)
	assert.Eq(t, false, retries[0].Retry.Restart)
	assert.True(t, retries[0].Retry.Wait > 0)

	events := eventsOf(t, repo, res.SessionID, store.EventModel)
	assert.Require(t, assert.Eq(t, 1, len(events)))
	assert.Eq(t, 1, attrInt(events[0].Attrs["retries"]))
	assert.Eq(t, "HTTP 503", events[0].Attrs["retry_reason"])
	assert.StrContains(t, traceLine(events[0]), "retries=1(HTTP 503, waited ")
	a := AnalyzeTrace(events)
	assert.Eq(t, 1, a.Retries)
	assert.True(t, a.RetryWaitMs >= 0)
}
