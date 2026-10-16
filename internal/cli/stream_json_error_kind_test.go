package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore/llm"
)

// TestStreamJSONErrorKind: the error line of a run that failed on a provider
// error names the kind of failure, so gofer can decide whether continuing the
// session makes sense - provider_transient for a cut stream whose retries ran
// out, provider_bad_response for an answer that cannot be used.
func TestStreamJSONErrorKind(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"a cut stream", `data: {"choices":[{"delta":{"content":"Hel`, "provider_transient"},
		{"an unusable answer", "data: {not json}\n", "provider_bad_response"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, c.body)
			}))
			t.Cleanup(server.Close)
			// Millisecond waits: the cut stream is retried, the bad answer is not.
			provider := llm.NewOpenAIProvider(llm.OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "m",
				Retry: llm.RetryPolicy{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}})
			runner, _ := newCLITestRunner(t, provider)

			var stdout, stderr bytes.Buffer
			err := executePersonalRun(context.Background(), runner, personalOptions{Prompt: "kind", OutputFormat: "stream-json"}, &stdout, &stderr)
			assert.Require(t, assert.True(t, err != nil))
			line := lastJSONLine(t, stdout.String())
			assert.Eq(t, "error", line["type"])
			assert.Eq(t, c.want, line["error_kind"])
		})
	}
}
