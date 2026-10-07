package axconfig

import (
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/testutil/assert"
)

func TestNormalizeLLMProviderSettings_PrefersAPITypeAndProviderName(t *testing.T) {
	settings := normalizeLLMProviderSettings(LLMProviderSettings{
		APIType:      "openai",
		ProviderName: "deepseek",
	})

	assert.Eq(t, "openai", settings.APIType)
	assert.Eq(t, "deepseek", settings.ProviderName)
}

func TestNormalizeLLMProviderSettings_DefaultsAPITypeToOpenAI(t *testing.T) {
	settings := normalizeLLMProviderSettings(LLMProviderSettings{})

	assert.Eq(t, "openai", settings.APIType)
	assert.Eq(t, "openai", settings.ProviderName)
}

func TestNormalizeLLMProviderSettings_DefaultsAnthropicProviderName(t *testing.T) {
	settings := normalizeLLMProviderSettings(LLMProviderSettings{
		APIType: "anthropic",
	})

	assert.Eq(t, "anthropic", settings.APIType)
	assert.Eq(t, "claude", settings.ProviderName)
}

func TestBuildLLMProvider_OpenAIAPITypeWithDeepSeekProviderName(t *testing.T) {
	provider, err := BuildLLMProviderFromSettings(LLMProviderSettings{
		APIType:      "openai",
		ProviderName: "deepseek",
		Model:        "deepseek-chat",
	})

	assert.NoErr(t, err)
	assert.NotNil(t, provider)
	assert.Eq(t, "deepseek", provider.Name())
}

func TestBuildLLMProvider_AnthropicAPITypeBuildsClaudeProvider(t *testing.T) {
	provider, err := BuildLLMProviderFromSettings(LLMProviderSettings{
		APIType:      "anthropic",
		ProviderName: "claude",
		Model:        "claude-sonnet-4-6",
	})

	assert.NoErr(t, err)
	assert.NotNil(t, provider)
	assert.Eq(t, "claude", provider.Name())
}

func TestBuildLLMProvider_UnsupportedAPITypeReturnsError(t *testing.T) {
	provider, err := BuildLLMProviderFromSettings(LLMProviderSettings{
		APIType:      "custom",
		ProviderName: "demo",
	})

	assert.Nil(t, provider)
	assert.Err(t, err)
}

func TestReadLLMProviderSettings_NormalizesConfigValues(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	_ = gkconfig.Set("llm.api_type", " OpenAI ")
	_ = gkconfig.Set("llm.provider_name", " DeepSeek ")
	_ = gkconfig.Set("llm.model", "deepseek-chat")
	_ = gkconfig.Set("llm.base_url", " https://api.deepseek.com/v1 ")

	settings := ReadLLMProviderSettings()

	assert.Eq(t, "openai", settings.APIType)
	assert.Eq(t, "deepseek", settings.ProviderName)
	assert.Eq(t, "deepseek-chat", settings.Model)
	assert.Eq(t, "https://api.deepseek.com/v1", settings.BaseURL)
}

func TestBuildLLMProvider_UsesNormalizedConfigSettings(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	_ = gkconfig.Set("llm.api_type", "openai")
	_ = gkconfig.Set("llm.provider_name", "deepseek")
	_ = gkconfig.Set("llm.model", "deepseek-chat")

	provider, err := BuildLLMProvider()

	assert.NoErr(t, err)
	assert.NotNil(t, provider)
	assert.Eq(t, "deepseek", provider.Name())
}

func TestReadLLMProviderSettingsOverridePrecedence(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	_ = gkconfig.Set("llm.api_type", "openai")
	_ = gkconfig.Set("llm.provider_name", "openai")
	_ = gkconfig.Set("llm.model", "config-model")
	t.Setenv("SUAG_MODEL", "env-model")
	t.Setenv("SUAG_PROVIDER", "deepseek")

	t.Run("environment", func(t *testing.T) {
		settings := ReadLLMProviderSettingsWithOverrides("", "")
		assert.Eq(t, "env-model", settings.Model)
		assert.Eq(t, "deepseek", settings.ProviderName)
	})
	t.Run("flags", func(t *testing.T) {
		settings := ReadLLMProviderSettingsWithOverrides("flag-model", "claude")
		assert.Eq(t, "flag-model", settings.Model)
		assert.Eq(t, "claude", settings.ProviderName)
		assert.Eq(t, "anthropic", settings.APIType)
	})
}
