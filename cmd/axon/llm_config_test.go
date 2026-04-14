package main

import (
	"testing"

	"github.com/gookit/config/v2"
	"github.com/gookit/goutil/testutil/assert"
)

func TestNormalizeLLMProviderSettings_PrefersAPITypeAndProviderName(t *testing.T) {
	settings := normalizeLLMProviderSettings(llmProviderSettings{
		APIType:      "openai",
		ProviderName: "deepseek",
		LegacyName:   "claude",
	})

	assert.Eq(t, "openai", settings.APIType)
	assert.Eq(t, "deepseek", settings.ProviderName)
	assert.Eq(t, "claude", settings.LegacyName)
}

func TestNormalizeLLMProviderSettings_FallsBackFromLegacyProvider(t *testing.T) {
	settings := normalizeLLMProviderSettings(llmProviderSettings{
		LegacyName: "deepseek",
	})

	assert.Eq(t, "openai", settings.APIType)
	assert.Eq(t, "deepseek", settings.ProviderName)
}

func TestNormalizeLLMProviderSettings_DefaultsAnthropicProviderName(t *testing.T) {
	settings := normalizeLLMProviderSettings(llmProviderSettings{
		APIType: "anthropic",
	})

	assert.Eq(t, "anthropic", settings.APIType)
	assert.Eq(t, "claude", settings.ProviderName)
}

func TestBuildLLMProvider_OpenAIAPITypeWithDeepSeekProviderName(t *testing.T) {
	provider, err := buildLLMProviderFromSettings(llmProviderSettings{
		APIType:      "openai",
		ProviderName: "deepseek",
		Model:        "deepseek-chat",
	})

	assert.NoErr(t, err)
	assert.NotNil(t, provider)
	assert.Eq(t, "deepseek", provider.Name())
}

func TestBuildLLMProvider_AnthropicAPITypeBuildsClaudeProvider(t *testing.T) {
	provider, err := buildLLMProviderFromSettings(llmProviderSettings{
		APIType:      "anthropic",
		ProviderName: "claude",
		Model:        "claude-sonnet-4-6",
	})

	assert.NoErr(t, err)
	assert.NotNil(t, provider)
	assert.Eq(t, "claude", provider.Name())
}

func TestBuildLLMProvider_UnsupportedAPITypeReturnsError(t *testing.T) {
	provider, err := buildLLMProviderFromSettings(llmProviderSettings{
		APIType:      "custom",
		ProviderName: "demo",
	})

	assert.Nil(t, provider)
	assert.Err(t, err)
}

func TestReadLLMProviderSettings_NormalizesConfigValues(t *testing.T) {
	config.Reset()
	t.Cleanup(config.Reset)

	_ = config.Set("llm.api_type", " OpenAI ")
	_ = config.Set("llm.provider_name", " DeepSeek ")
	_ = config.Set("llm.model", "deepseek-chat")
	_ = config.Set("llm.base_url", " https://api.deepseek.com/v1 ")

	settings := readLLMProviderSettings()

	assert.Eq(t, "openai", settings.APIType)
	assert.Eq(t, "deepseek", settings.ProviderName)
	assert.Eq(t, "deepseek-chat", settings.Model)
	assert.Eq(t, "https://api.deepseek.com/v1", settings.BaseURL)
}
