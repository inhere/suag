package llm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	agentcoreapi "github.com/inhere/suga/pkg/agentcore/api"
)

// TestProviderRedirectsAreRejected covers both request paths of every protocol.
// Reverse verification: removing CheckRedirect would make B receive the request
// (including x-api-key for Claude), so the zero-request assertion would fail.
func TestProviderRedirectsAreRejected(t *testing.T) {
	req := &agentcoreapi.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "hi"}}}
	for _, tc := range []struct {
		name   string
		status int
		make   func(string) agentcoreapi.Provider
		stream func(agentcoreapi.Provider) error
	}{
		{"claude generate", http.StatusFound, func(url string) agentcoreapi.Provider {
			return NewClaudeProvider(ClaudeConfig{APIKey: "secret", BaseURL: url, Retry: fastRetry})
		}, nil},
		{"claude stream", http.StatusTemporaryRedirect, func(url string) agentcoreapi.Provider {
			return NewClaudeProvider(ClaudeConfig{APIKey: "secret", BaseURL: url, Retry: fastRetry})
		}, func(p agentcoreapi.Provider) error {
			_, err := p.(agentcoreapi.StreamingProvider).Stream(context.Background(), req, nil)
			return err
		}},
		{"openai generate", http.StatusFound, func(url string) agentcoreapi.Provider {
			return NewOpenAIProvider(OpenAIConfig{APIKey: "secret", BaseURL: url + "/v1", Retry: fastRetry})
		}, nil},
		{"openai stream", http.StatusTemporaryRedirect, func(url string) agentcoreapi.Provider {
			return NewOpenAIProvider(OpenAIConfig{APIKey: "secret", BaseURL: url + "/v1", Retry: fastRetry})
		}, func(p agentcoreapi.Provider) error {
			_, err := p.(agentcoreapi.StreamingProvider).Stream(context.Background(), req, nil)
			return err
		}},
		{"responses generate", http.StatusFound, func(url string) agentcoreapi.Provider {
			return NewResponsesProvider(ResponsesConfig{OpenAIConfig: OpenAIConfig{APIKey: "secret", BaseURL: url + "/v1", Retry: fastRetry}})
		}, nil},
		{"responses stream", http.StatusTemporaryRedirect, func(url string) agentcoreapi.Provider {
			return NewResponsesProvider(ResponsesConfig{OpenAIConfig: OpenAIConfig{APIKey: "secret", BaseURL: url + "/v1", Retry: fastRetry}})
		}, func(p agentcoreapi.Provider) error {
			_, err := p.(agentcoreapi.StreamingProvider).Stream(context.Background(), req, nil)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bCalls := 0
			b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				bCalls++
				_, _ = io.Copy(io.Discard, r.Body)
				w.WriteHeader(http.StatusOK)
			}))
			defer b.Close()
			a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", b.URL+r.URL.Path)
				w.WriteHeader(tc.status)
			}))
			defer a.Close()

			var err error
			provider := tc.make(a.URL)
			if tc.stream != nil {
				err = tc.stream(provider)
			} else {
				_, err = provider.Generate(context.Background(), req)
			}
			assert.Require(t, assert.Err(t, err))
			assert.StrContains(t, err.Error(), fmt.Sprintf("%d", tc.status))
			assert.StrContains(t, err.Error(), strings.TrimPrefix(b.URL, "http://"))
			assert.Eq(t, ErrorKindRejected, ErrorKind(err))
			assert.Eq(t, 0, bCalls)
		})
	}
}
