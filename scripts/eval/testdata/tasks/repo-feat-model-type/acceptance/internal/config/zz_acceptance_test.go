package axconfig

// Acceptance test injected after the run (the agent does not see it).

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"

	agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"
	"github.com/inhere/suag/pkg/agentcore/llm"
)

func TestAcceptModelTypeOverride(t *testing.T) {
	doc := `
model: cc/deepseek-v4.1-flash
providers:
  cc:
    type: openai
    base_url: https://cc.example/v1
    models:
      deepseek-v4.1-flash:
        model_id: deepseek/deepseek-v4.1-flash
      sonnet:
        type: anthropic
        base_url: ${CC_ANTHROPIC_URL}
        model_id: claude-sonnet-4-5
`
	t.Run("a model's type and base_url win over the provider's", func(t *testing.T) {
		loadYAML(t, doc)
		var path string
		var sent map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path = r.URL.Path
			_ = json.NewDecoder(r.Body).Decode(&sent)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`)
		}))
		defer srv.Close()
		t.Setenv("CC_ANTHROPIC_URL", srv.URL)

		s, err := ReadModelSettings("cc/sonnet")
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, ProviderTypeAnthropic, s.Main.Provider.Type)
		assert.Eq(t, srv.URL, s.Main.Provider.BaseURL)
		p, err := BuildProvider(s.Main)
		assert.Require(t, assert.NoErr(t, err))
		_, err = p.Generate(context.Background(), &llm.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "hi"}}})
		assert.NoErr(t, err)
		assert.Eq(t, "/v1/messages", path) // the Anthropic protocol
		assert.Eq(t, "claude-sonnet-4-5", sent["model"])
	})
	t.Run("other models keep the provider's", func(t *testing.T) {
		loadYAML(t, doc)
		s, err := ReadModelSettings("")
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, ProviderTypeOpenAI, s.Main.Provider.Type)
		assert.Eq(t, "https://cc.example/v1", s.Main.Provider.BaseURL)
	})
	t.Run("an unknown model type is rejected", func(t *testing.T) {
		loadYAML(t, strings.Replace(doc, "type: anthropic", "type: gemini", 1))
		_, err := ReadProviders()
		assert.ErrMsg(t, err, `providers.cc.models.sonnet: unsupported type "gemini" (want openai|openai-responses|anthropic)`)
	})
}
