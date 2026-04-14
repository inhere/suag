package llm

import (
	"testing"

	"github.com/gookit/goutil/testutil/assert"
	agentcoreapi "github.com/inhere/axon/pkg/agentcore/api"
)

func TestOpenAIProvider_ImplementsAgentcoreProviderContracts(t *testing.T) {
	provider := NewOpenAIProvider(OpenAIConfig{})

	var base agentcoreapi.Provider = provider
	var streaming agentcoreapi.StreamingProvider = provider
	var embedding agentcoreapi.EmbeddingProvider = provider

	assert.NotNil(t, base)
	assert.NotNil(t, streaming)
	assert.NotNil(t, embedding)
	assert.Eq(t, "openai", provider.Name())
}

func TestClaudeProvider_DoesNotRequireEmbeddingSupport(t *testing.T) {
	provider := NewClaudeProvider(ClaudeConfig{})

	var base agentcoreapi.Provider = provider
	var streaming agentcoreapi.StreamingProvider = provider

	assert.NotNil(t, base)
	assert.NotNil(t, streaming)
	assert.Eq(t, "claude", provider.Name())
}

func TestDeepSeekProvider_ImplementsEmbeddingProvider(t *testing.T) {
	provider := NewDeepSeekProvider("", "")

	var embedding agentcoreapi.EmbeddingProvider = provider

	assert.NotNil(t, embedding)
	assert.Eq(t, "deepseek", provider.Name())
}
