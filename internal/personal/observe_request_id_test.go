package personal

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore/llm"
)

func TestModelEventRecordsRequestIDOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("x-request-id", "trace-success")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(server.Close)
	repo := openObserveRepo(t)
	runner, err := New(Config{Repository: repo, Provider: llm.NewOpenAIProvider(llm.OpenAIConfig{
		APIKey: "k", BaseURL: server.URL, Model: "m",
	})})
	assert.NoErr(t, err)
	res, err := runner.Run(context.Background(), RunRequest{Prompt: "hi"})
	assert.Require(t, assert.NoErr(t, err))
	events := eventsOf(t, repo, res.SessionID, store.EventModel)
	assert.Require(t, assert.Len(t, events, 1))
	assert.Eq(t, "trace-success", events[0].Attrs["request_id"])
}
