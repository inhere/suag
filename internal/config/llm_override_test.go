package axconfig

import (
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
)

// setDeepSeekFileConfig writes a DeepSeek provider block into the config,
// including its credential and endpoint, to be used as the file baseline.
func setDeepSeekFileConfig(t *testing.T) {
	t.Helper()
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	_ = gkconfig.Set("llm.api_type", "openai")
	_ = gkconfig.Set("llm.provider_name", "deepseek")
	_ = gkconfig.Set("llm.model", "deepseek-chat")
	_ = gkconfig.Set("llm.api_key", "sk-deepseek")
	_ = gkconfig.Set("llm.base_url", "https://api.deepseek.com/v1")
}

// clearOverrideEnv isolates a test from the caller's shell overrides and keys.
func clearOverrideEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SUAG_MODEL", "")
	t.Setenv("SUAG_PROVIDER", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
}

func TestReadLLMProviderSettings_ProviderOverrideDoesNotLeakFileCredentials(t *testing.T) {
	t.Run("deepseek file to claude flag", func(t *testing.T) {
		setDeepSeekFileConfig(t)
		clearOverrideEnv(t)
		t.Setenv("ANTHROPIC_API_KEY", "sk-anthropic-env")

		settings := ReadLLMProviderSettingsWithOverrides("", "claude")
		assert.Eq(t, "anthropic", settings.APIType)
		assert.Eq(t, "claude", settings.ProviderName)
		// The DeepSeek key and endpoint must not be sent to Anthropic; the
		// empty key lets the build step read ANTHROPIC_API_KEY.
		assert.Eq(t, "", settings.APIKey)
		assert.Eq(t, "", settings.BaseURL)
		// The file model belongs to DeepSeek, so the Claude default applies.
		assert.Eq(t, "", settings.Model)

		provider, err := BuildLLMProviderFromSettings(settings)
		assert.NoErr(t, err)
		assert.Eq(t, "claude", provider.Name())
	})

	t.Run("deepseek file to openai flag drops deepseek base url", func(t *testing.T) {
		setDeepSeekFileConfig(t)
		clearOverrideEnv(t)

		settings := ReadLLMProviderSettingsWithOverrides("", "openai")
		assert.Eq(t, "openai", settings.APIType)
		assert.Eq(t, "openai", settings.ProviderName)
		assert.Eq(t, "", settings.APIKey)
		assert.Eq(t, "", settings.BaseURL)
		assert.Eq(t, "", settings.Model)

		provider, err := BuildLLMProviderFromSettings(settings)
		assert.NoErr(t, err)
		assert.Eq(t, "openai", provider.Name())
	})

	t.Run("deepseek file to claude via env override", func(t *testing.T) {
		setDeepSeekFileConfig(t)
		clearOverrideEnv(t)
		t.Setenv("SUAG_PROVIDER", "claude")

		settings := ReadLLMProviderSettingsWithOverrides("", "")
		assert.Eq(t, "claude", settings.ProviderName)
		assert.Eq(t, "", settings.APIKey)
		assert.Eq(t, "", settings.BaseURL)
	})

	t.Run("explicit model survives provider switch", func(t *testing.T) {
		setDeepSeekFileConfig(t)
		clearOverrideEnv(t)

		settings := ReadLLMProviderSettingsWithOverrides("gpt-4o-mini", "openai")
		assert.Eq(t, "gpt-4o-mini", settings.Model)
		assert.Eq(t, "", settings.APIKey)
		assert.Eq(t, "", settings.BaseURL)
	})

	t.Run("explicit env model survives provider switch", func(t *testing.T) {
		setDeepSeekFileConfig(t)
		clearOverrideEnv(t)
		t.Setenv("SUAG_MODEL", "claude-env-model")

		settings := ReadLLMProviderSettingsWithOverrides("", "claude")
		assert.Eq(t, "claude-env-model", settings.Model)
	})

	t.Run("same provider keeps file values", func(t *testing.T) {
		setDeepSeekFileConfig(t)
		clearOverrideEnv(t)

		settings := ReadLLMProviderSettingsWithOverrides("", "deepseek")
		assert.Eq(t, "deepseek", settings.ProviderName)
		assert.Eq(t, "sk-deepseek", settings.APIKey)
		assert.Eq(t, "https://api.deepseek.com/v1", settings.BaseURL)
		assert.Eq(t, "deepseek-chat", settings.Model)
	})

	t.Run("no override keeps file values", func(t *testing.T) {
		setDeepSeekFileConfig(t)
		clearOverrideEnv(t)

		settings := ReadLLMProviderSettingsWithOverrides("", "")
		assert.Eq(t, "deepseek", settings.ProviderName)
		assert.Eq(t, "sk-deepseek", settings.APIKey)
		assert.Eq(t, "https://api.deepseek.com/v1", settings.BaseURL)
		assert.Eq(t, "deepseek-chat", settings.Model)
	})

	t.Run("anthropic alias of claude file keeps file key", func(t *testing.T) {
		gkconfig.Reset()
		t.Cleanup(gkconfig.Reset)
		clearOverrideEnv(t)
		_ = gkconfig.Set("llm.api_type", "anthropic")
		_ = gkconfig.Set("llm.provider_name", "claude")
		_ = gkconfig.Set("llm.model", "claude-sonnet-4-6")
		_ = gkconfig.Set("llm.api_key", "sk-claude-file")

		settings := ReadLLMProviderSettingsWithOverrides("", "anthropic")
		assert.Eq(t, "anthropic", settings.APIType)
		assert.Eq(t, "sk-claude-file", settings.APIKey)
		assert.Eq(t, "claude-sonnet-4-6", settings.Model)
	})
}

func TestCompactModelSettings(t *testing.T) {
	t.Run("unset uses the current model", func(t *testing.T) {
		setDeepSeekFileConfig(t)
		base := ReadLLMProviderSettingsWithOverrides("", "")
		_, ok := CompactModelSettings(base)
		assert.Eq(t, false, ok)
	})
	t.Run("configured swaps only the model", func(t *testing.T) {
		setDeepSeekFileConfig(t)
		_ = gkconfig.Set("llm.compact_model", "deepseek-lite")
		base := ReadLLMProviderSettingsWithOverrides("", "")
		got, ok := CompactModelSettings(base)
		assert.Eq(t, true, ok)
		assert.Eq(t, "deepseek-lite", got.Model)
		assert.Eq(t, base.APIKey, got.APIKey)
		assert.Eq(t, base.BaseURL, got.BaseURL)
		assert.Eq(t, "deepseek-chat", base.Model)
	})
	t.Run("same as current model is not a separate provider", func(t *testing.T) {
		setDeepSeekFileConfig(t)
		_ = gkconfig.Set("llm.compact_model", "deepseek-chat")
		_, ok := CompactModelSettings(ReadLLMProviderSettingsWithOverrides("", ""))
		assert.Eq(t, false, ok)
	})
}
