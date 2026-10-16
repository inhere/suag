package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/gookit/goutil/x/assert"

	agentcoreapi "github.com/inhere/suga/pkg/agentcore/api"
)

// TestErrorKind: the hosts (trace turn event, stream-json error_kind) tell a
// cut answer a new attempt may fix from a provider answer that was unusable, so
// gofer can decide whether continuing the session makes sense.
func TestErrorKind(t *testing.T) {
	cut := streamTruncatedError("openai", cutLine)
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"a cut stream", cut, ErrorKindTransient},
		{"a cut stream after the retries ran out", fmt.Errorf("openai stream broke 3 times: %w", cut), ErrorKindTransient},
		{"a body that ended early", streamBodyError("openai", errors.New("unexpected EOF")), ErrorKindTransient},
		{"a dropped connection", streamBodyError("openai", errors.New("connection reset by peer")), ErrorKindTransient},
		{"a stream event that is not JSON", badResponsef("parse openai stream chunk: %w", errors.New("invalid character 'n'")), ErrorKindBadResponse},
		{"a whole response that is not JSON", badResponsef("parse claude response: %w", errors.New("unexpected end of JSON input")), ErrorKindBadResponse},
		{"an api error", badResponsef("openai api error: %s", "bad model"), ErrorKindBadResponse},
		{"an api error over the pool", fmt.Errorf("all providers failed: %w", badResponsef("openai api error: %s", "bad model")), ErrorKindBadResponse},
		{"the runtime's provider wrap", &agentcoreapi.ProviderError{Err: cut}, ErrorKindTransient},
		{"a cancelled turn", context.Canceled, ""},
		{"any other error", errors.New("boom"), ""},
		{"no error", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Eq(t, c.want, ErrorKind(c.err))
		})
	}
}

// TestErrorKindOnProviderStreams runs the classifier over the errors the
// providers really return (including the runtime's wrap), not only over
// constructed ones.
func TestErrorKindOnProviderStreams(t *testing.T) {
	req := &agentcoreapi.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "hi"}}}
	provider := func(url string) *OpenAIProvider {
		return NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: url, Model: "m", Retry: fastRetry})
	}

	t.Run("a cut stream is transient", func(t *testing.T) {
		var hits atomic.Int32
		server := cutStreamServer(t, `data: {"choices":[{"delta":{"content":"x`, 100, "", &hits)
		_, err := provider(server.URL).Stream(context.Background(), req, nil)
		assert.Require(t, assert.True(t, err != nil))
		assert.Eq(t, ErrorKindTransient, ErrorKind(&agentcoreapi.ProviderError{Err: err}))
	})
	t.Run("a bad chunk is a bad response", func(t *testing.T) {
		var hits atomic.Int32
		server := cutStreamServer(t, "data: {not json}\n", 100, "", &hits)
		_, err := provider(server.URL).Stream(context.Background(), req, nil)
		assert.Require(t, assert.True(t, err != nil))
		assert.Eq(t, ErrorKindBadResponse, ErrorKind(&agentcoreapi.ProviderError{Err: err}))
	})
	t.Run("an api error of a non-streamed request is a bad response", func(t *testing.T) {
		server := sseServer(t, http.StatusBadRequest, `{"error":{"message":"bad model"}}`, nil)
		_, err := provider(server.URL).Generate(context.Background(), req)
		assert.Require(t, assert.True(t, err != nil))
		assert.Eq(t, ErrorKindBadResponse, ErrorKind(&agentcoreapi.ProviderError{Err: err}))
	})
}
