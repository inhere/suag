package main

import (
	"testing"

	"github.com/gookit/config/v2"
	"github.com/gookit/goutil/testutil/assert"
)

func TestBuildLLMProvider_OpenAIAPITypeWithDeepSeekProviderName(t *testing.T) {
	config.Reset()
	t.Cleanup(config.Reset)

	_ = config.Set("llm.api_type", "openai")
	_ = config.Set("llm.provider_name", "deepseek")
	_ = config.Set("llm.model", "deepseek-chat")

	provider, err := buildLLMProvider()

	assert.NoErr(t, err)
	assert.NotNil(t, provider)
	assert.Eq(t, "deepseek", provider.Name())
}

func TestBuildLLMProvider_AnthropicAPITypeBuildsClaudeProvider(t *testing.T) {
	config.Reset()
	t.Cleanup(config.Reset)

	_ = config.Set("llm.api_type", "anthropic")
	_ = config.Set("llm.provider_name", "claude")
	_ = config.Set("llm.model", "claude-sonnet-4-6")

	provider, err := buildLLMProvider()

	assert.NoErr(t, err)
	assert.NotNil(t, provider)
	assert.Eq(t, "claude", provider.Name())
}
