package axconfig

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gookit/goutil/x/assert"

	agentcoreapi "github.com/inhere/suga/pkg/agentcore/api"
	"github.com/inhere/suga/pkg/agentcore/llm"
)

// sentBody builds spec's provider against a fake server and returns the JSON
// body of one request.
func sentBody(t *testing.T, spec ModelSpec) map[string]any {
	t.Helper()
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&sent)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()
	spec.Provider.BaseURL = srv.URL
	p, err := BuildProvider(spec)
	assert.Require(t, assert.NoErr(t, err))
	_, err = p.Generate(context.Background(), &llm.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "hi"}}})
	assert.Require(t, assert.NoErr(t, err))
	return sent
}

func TestThinking(t *testing.T) {
	doc := `
model: cc/fast
providers:
  cc:
    type: openai
    thinking: HIGH
    models:
      fast:
        thinking: low
      deep: {}
`
	t.Run("parse", func(t *testing.T) {
		for _, v := range ThinkingLevels {
			got, err := ParseThinking(v)
			assert.NoErr(t, err)
			assert.Eq(t, v, got)
		}
		got, err := ParseThinking(" High ")
		assert.NoErr(t, err)
		assert.Eq(t, "high", got)
		_, err = ParseThinking("extreme")
		assert.ErrMsgContains(t, err, `unknown thinking level "extreme"`)
	})
	t.Run("override, then model, then provider", func(t *testing.T) {
		loadYAML(t, doc)
		s, err := ReadModelSettings("")
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "low", s.Main.ThinkingLevel())
		s.Main.Thinking = "max"
		assert.Eq(t, "max", s.Main.ThinkingLevel())
		deep, err := ReadModelSettings("cc/deep")
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "high", deep.Main.ThinkingLevel()) // provider value, normalized
	})
	t.Run("invalid config levels are rejected", func(t *testing.T) {
		loadYAML(t, "providers:\n  cc:\n    type: openai\n    thinking: huge\n")
		_, err := ReadProviders()
		assert.ErrMsgContains(t, err, "providers.cc.thinking: unknown thinking level")
		loadYAML(t, "providers:\n  cc:\n    type: openai\n    models:\n      m:\n        thinking: huge\n")
		_, err = ReadProviders()
		assert.ErrMsgContains(t, err, "providers.cc.models.m.thinking: unknown thinking level")
	})
	t.Run("sent as reasoning_effort", func(t *testing.T) {
		loadYAML(t, doc)
		s, err := ReadModelSettings("")
		assert.Require(t, assert.NoErr(t, err))
		for level, want := range map[string]any{"high": "high", "xhigh": "xhigh", "off": "off", "minimal": "minimal", "auto": nil} {
			spec := s.Main
			spec.Thinking = level
			assert.Eq(t, want, sentBody(t, spec)["reasoning_effort"], level)
		}
		loadYAML(t, "providers:\n  cc:\n    type: openai\n")
		plain, err := ReadModelSettings("cc/m")
		assert.Require(t, assert.NoErr(t, err))
		_, present := sentBody(t, plain.Main)["reasoning_effort"]
		assert.False(t, present) // nothing configured: the provider default
	})
}
