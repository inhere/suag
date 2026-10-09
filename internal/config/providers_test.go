package axconfig

import (
	"strings"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/config/v2/yaml"
	"github.com/gookit/goutil/x/assert"
)

// loadYAML replaces the global config with the given YAML and isolates the
// provider-related environment from the caller's shell.
func loadYAML(t *testing.T, doc string) {
	t.Helper()
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	gkconfig.AddDriver(yaml.Driver)
	if doc != "" {
		assert.NoErr(t, gkconfig.LoadSources(gkconfig.Yaml, []byte(doc)))
	}
	for _, k := range []string{"SUAG_MODEL", "SUAG_PROVIDER", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "DEEPSEEK_API_KEY"} {
		t.Setenv(k, "")
	}
}

func TestReadProviders(t *testing.T) {
	t.Run("presets with env keys", func(t *testing.T) {
		loadYAML(t, "")
		t.Setenv("DEEPSEEK_API_KEY", "sk-ds")
		ps, err := ReadProviders()
		assert.NoErr(t, err)
		assert.Eq(t, "openai", ps["openai"].Type)
		assert.Eq(t, "https://api.openai.com/v1", ps["openai"].BaseURL)
		assert.Eq(t, "anthropic", ps["anthropic"].Type)
		assert.Eq(t, "https://api.anthropic.com", ps["anthropic"].BaseURL)
		assert.Eq(t, "openai", ps["deepseek"].Type)
		assert.Eq(t, "https://api.deepseek.com/v1", ps["deepseek"].BaseURL)
		assert.Eq(t, "sk-ds", ps["deepseek"].APIKey)
		assert.Eq(t, "", ps["openai"].APIKey)
	})

	t.Run("user overrides preset field by field", func(t *testing.T) {
		loadYAML(t, `
providers:
  deepseek:
    base_url: https://proxy.example/v1
    api_key: sk-file
    type: ""
`)
		t.Setenv("DEEPSEEK_API_KEY", "sk-env")
		ps, err := ReadProviders()
		assert.NoErr(t, err)
		assert.Eq(t, "https://proxy.example/v1", ps["deepseek"].BaseURL)
		assert.Eq(t, "sk-file", ps["deepseek"].APIKey)
		assert.Eq(t, "openai", ps["deepseek"].Type) // empty field does not override
	})

	t.Run("preset key from env when user omits it", func(t *testing.T) {
		loadYAML(t, "providers:\n  openai:\n    base_url: https://x/v1\n")
		t.Setenv("OPENAI_API_KEY", "sk-env")
		ps, err := ReadProviders()
		assert.NoErr(t, err)
		assert.Eq(t, "sk-env", ps["openai"].APIKey)
		assert.Eq(t, "https://x/v1", ps["openai"].BaseURL)
	})

	t.Run("custom provider needs type", func(t *testing.T) {
		loadYAML(t, "providers:\n  openrouter:\n    base_url: https://openrouter.ai/api/v1\n")
		_, err := ReadProviders()
		assert.Err(t, err)
		assert.StrContains(t, err.Error(), "providers.openrouter")
		assert.StrContains(t, err.Error(), "type is required")
	})

	t.Run("custom provider invalid type", func(t *testing.T) {
		loadYAML(t, "providers:\n  x:\n    type: gemini\n")
		_, err := ReadProviders()
		assert.Err(t, err)
		assert.StrContains(t, err.Error(), "unsupported type")
	})

	t.Run("headers placeholder and model context window", func(t *testing.T) {
		loadYAML(t, `
providers:
  proxy:
    type: openai
    base_url: https://p/v1
    headers:
      X-Session: "{{session_id}}"
    models:
      gpt-4.1:
        context_window: 64000
`)
		ps, err := ReadProviders()
		assert.NoErr(t, err)
		assert.Eq(t, "{{session_id}}", ps["proxy"].Headers["X-Session"])
		assert.Eq(t, 64000, ps["proxy"].Models["gpt-4.1"].ContextWindow)
	})
}

func TestResolveModel(t *testing.T) {
	loadYAML(t, "providers:\n  proxy:\n    type: openai\n")
	ps, err := ReadProviders()
	assert.NoErr(t, err)

	cases := []struct {
		name, ref, def, wantProv, wantModel, wantErr string
	}{
		{"prefixed preset", "deepseek/deepseek-chat", "", "deepseek", "deepseek-chat", ""},
		{"prefixed custom", "proxy/gpt-4o", "openai", "proxy", "gpt-4o", ""},
		{"bare uses default", "gpt-4o", "openai", "openai", "gpt-4o", ""},
		{"undefined prefix is part of model", "openrouter/meta/llama-3", "proxy", "proxy", "openrouter/meta/llama-3", ""},
		{"only first slash splits", "proxy/meta/llama-3", "openai", "proxy", "meta/llama-3", ""},
		{"empty", "  ", "openai", "", "", "empty"},
		{"bare without default", "gpt-4o", "", "", "", "no provider"},
		{"default undefined", "gpt-4o", "nope", "", "", "not defined"},
		{"missing model id", "openai/", "", "", "", "no model id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := ResolveModel(tc.ref, ps, tc.def)
			if tc.wantErr != "" {
				assert.Err(t, err)
				assert.StrContains(t, err.Error(), tc.wantErr)
				return
			}
			assert.NoErr(t, err)
			assert.Eq(t, tc.wantProv, spec.Provider.ID)
			assert.Eq(t, tc.wantModel, spec.Model)
			assert.Eq(t, tc.wantProv+"/"+tc.wantModel, spec.Ref())
		})
	}
}

func TestReadModelSettings(t *testing.T) {
	const doc = `
model: deepseek/deepseek-chat
compact_model: deepseek-lite
fallback_models:
  - openai/gpt-4o
  - deepseek-reasoner
`
	t.Run("config values", func(t *testing.T) {
		loadYAML(t, doc)
		s, err := ReadModelSettings("")
		assert.NoErr(t, err)
		assert.Eq(t, "deepseek/deepseek-chat", s.Main.Ref())
		assert.NotNil(t, s.Compact)
		assert.Eq(t, "deepseek/deepseek-lite", s.Compact.Ref()) // bare uses main provider
		assert.Eq(t, 2, len(s.Fallbacks))
		assert.Eq(t, "openai/gpt-4o", s.Fallbacks[0].Ref())
		assert.Eq(t, "deepseek/deepseek-reasoner", s.Fallbacks[1].Ref())
	})

	t.Run("priority flag over env over config", func(t *testing.T) {
		loadYAML(t, doc)
		t.Setenv("SUAG_MODEL", "anthropic/claude-sonnet-4-6")
		s, err := ReadModelSettings("openai/gpt-4o")
		assert.NoErr(t, err)
		assert.Eq(t, "openai/gpt-4o", s.Main.Ref())

		s, err = ReadModelSettings("")
		assert.NoErr(t, err)
		assert.Eq(t, "anthropic/claude-sonnet-4-6", s.Main.Ref())
		// bare compact model follows the new main provider
		assert.Eq(t, "anthropic/deepseek-lite", s.Compact.Ref())
	})

	t.Run("compact unset is nil", func(t *testing.T) {
		loadYAML(t, "model: openai/gpt-4o\n")
		s, err := ReadModelSettings("")
		assert.NoErr(t, err)
		assert.Nil(t, s.Compact)
		assert.Eq(t, 0, len(s.Fallbacks))
	})

	t.Run("model id containing slash", func(t *testing.T) {
		loadYAML(t, "providers:\n  openrouter:\n    type: openai\nmodel: openrouter/meta/llama-3\n")
		s, err := ReadModelSettings("")
		assert.NoErr(t, err)
		assert.Eq(t, "openrouter", s.Main.Provider.ID)
		assert.Eq(t, "meta/llama-3", s.Main.Model)
	})

	t.Run("model not configured", func(t *testing.T) {
		loadYAML(t, "")
		_, err := ReadModelSettings("")
		assert.Err(t, err)
		assert.StrContains(t, err.Error(), "model: <provider>/<model>")
	})

	t.Run("bare main model rejected", func(t *testing.T) {
		loadYAML(t, "model: gpt-4o\n")
		_, err := ReadModelSettings("")
		assert.Err(t, err)
		assert.StrContains(t, err.Error(), "no provider")
	})

	t.Run("bad fallback names its position", func(t *testing.T) {
		loadYAML(t, "model: openai/gpt-4o\nfallback_models: [\"\"]\n")
		_, err := ReadModelSettings("")
		assert.Err(t, err)
		assert.StrContains(t, err.Error(), "fallback_models[0]")
	})
}

func TestCheckLegacyConfig(t *testing.T) {
	t.Run("llm section", func(t *testing.T) {
		loadYAML(t, "llm:\n  model: gpt-4o\nmodel: openai/gpt-4o\n")
		err := CheckLegacyConfig()
		assert.Err(t, err)
		assert.StrContains(t, err.Error(), "llm: section is no longer supported")
		assert.StrContains(t, err.Error(), "model: <provider>/<model>")
		assert.StrContains(t, err.Error(), "providers:")
		_, err = ReadModelSettings("")
		assert.Err(t, err)
	})

	t.Run("SUAG_PROVIDER", func(t *testing.T) {
		loadYAML(t, "model: openai/gpt-4o\n")
		t.Setenv("SUAG_PROVIDER", "deepseek")
		err := CheckLegacyConfig()
		assert.Err(t, err)
		assert.True(t, strings.Contains(err.Error(), "SUAG_MODEL=<provider>/<model>"))
		_, err = ReadModelSettings("")
		assert.Err(t, err)
	})

	t.Run("clean", func(t *testing.T) {
		loadYAML(t, "model: openai/gpt-4o\n")
		assert.NoErr(t, CheckLegacyConfig())
	})
}

func TestContextWindowFor(t *testing.T) {
	loadYAML(t, `
providers:
  deepseek:
    models:
      deepseek-chat:
        context_window: 64000
llm:
  context_window: 99999
`)
	ps, err := ReadProviders()
	assert.NoErr(t, err)

	cases := []struct {
		name, ref string
		want      int
	}{
		{"configured wins", "deepseek/deepseek-chat", 64000},
		{"inferred from name", "deepseek/deepseek-reasoner", 128000},
		{"inferred claude", "anthropic/claude-sonnet-4-6", 200000},
		{"unknown default, ignores llm.context_window", "openai/mystery", DefaultContextWindow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := ResolveModel(tc.ref, ps, "")
			assert.NoErr(t, err)
			assert.Eq(t, tc.want, ContextWindowFor(spec))
		})
	}
}
