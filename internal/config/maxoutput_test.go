package axconfig

import (
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// TestResolveMaxOutputTokens covers the precedence
// --max-output-tokens > SUGA_MAX_OUTPUT_TOKENS > models.<m>.max_output_tokens
// > providers.<id>.max_output_tokens.
func TestResolveMaxOutputTokens(t *testing.T) {
	provider := ProviderConfig{
		ID: "gw", Type: ProviderTypeOpenAI, MaxOutputTokens: 16000,
		Models: map[string]ModelConfig{"m1": {MaxOutputTokens: 64000}, "m2": {}},
	}
	spec := func(model string) ModelSpec { return ModelSpec{Provider: provider, Model: model} }

	t.Run("the model value wins over the provider's", func(t *testing.T) {
		n, err := ResolveMaxOutputTokens(0, spec("m1"))
		assert.NoErr(t, err)
		assert.Eq(t, 64000, n)
	})
	t.Run("the provider value covers its other models", func(t *testing.T) {
		n, err := ResolveMaxOutputTokens(0, spec("m2"))
		assert.NoErr(t, err)
		assert.Eq(t, 16000, n)
	})
	t.Run("without any configuration nothing is sent", func(t *testing.T) {
		n, err := ResolveMaxOutputTokens(0, ModelSpec{Provider: ProviderConfig{ID: "p"}, Model: "m"})
		assert.NoErr(t, err)
		assert.Eq(t, 0, n)
		assert.Eq(t, 0, MaxOutputTokensFor(ModelSpec{Provider: ProviderConfig{ID: "p"}, Model: "m"}))
	})
	t.Run("the environment overrides the configuration", func(t *testing.T) {
		t.Setenv("SUGA_MAX_OUTPUT_TOKENS", "4000")
		n, err := ResolveMaxOutputTokens(0, spec("m1"))
		assert.NoErr(t, err)
		assert.Eq(t, 4000, n)
	})
	t.Run("the flag overrides the environment", func(t *testing.T) {
		t.Setenv("SUGA_MAX_OUTPUT_TOKENS", "4000")
		n, err := ResolveMaxOutputTokens(2000, spec("m1"))
		assert.NoErr(t, err)
		assert.Eq(t, 2000, n)
	})
	t.Run("a value below 1 is an error", func(t *testing.T) {
		_, err := ResolveMaxOutputTokens(-1, spec("m1"))
		assert.ErrMsgContains(t, err, "--max-output-tokens must be a whole number >= 1, got -1")
		t.Setenv("SUGA_MAX_OUTPUT_TOKENS", "0")
		_, err = ResolveMaxOutputTokens(0, spec("m1"))
		assert.ErrMsgContains(t, err, "SUGA_MAX_OUTPUT_TOKENS must be a whole number >= 1, got \"0\"")
		t.Setenv("SUGA_MAX_OUTPUT_TOKENS", "abc")
		_, err = ResolveMaxOutputTokens(0, spec("m1"))
		assert.ErrMsgContains(t, err, "SUGA_MAX_OUTPUT_TOKENS")
	})
}

// TestReadProvidersMaxOutputTokens: providers.<id>.max_output_tokens and
// providers.<id>.models.<m>.max_output_tokens are read from the config.
func TestReadProvidersMaxOutputTokens(t *testing.T) {
	loadYAML(t, `
model: gw/m1
providers:
  gw:
    type: openai
    base_url: https://gw.example/v1
    max_output_tokens: 16000
    models:
      m1:
        max_output_tokens: 64000
      m2:
        context_window: 128000
`)
	providers, err := ReadProviders()
	assert.NoErr(t, err)
	assert.Eq(t, 16000, providers["gw"].MaxOutputTokens)
	assert.Eq(t, 64000, providers["gw"].Models["m1"].MaxOutputTokens)
	assert.Eq(t, 0, providers["gw"].Models["m2"].MaxOutputTokens)

	t.Run("the resolved model sees them through ResolveModel", func(t *testing.T) {
		settings, err := ReadModelSettings("")
		assert.NoErr(t, err)
		n, err := ResolveMaxOutputTokens(0, settings.Main)
		assert.NoErr(t, err)
		assert.Eq(t, 64000, n)
		n, err = ResolveMaxOutputTokens(0, ModelSpec{Provider: settings.Main.Provider, Model: "m2"})
		assert.NoErr(t, err)
		assert.Eq(t, 16000, n)
	})
}
