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

func TestOpenAIProvider_ParseResponse_RejectsInvalidToolArguments(t *testing.T) {
	provider := NewOpenAIProvider(OpenAIConfig{})

	_, err := provider.parseResponse([]byte(`{
		"choices": [{
			"message": {
				"tool_calls": [{
					"id": "call-1",
					"function": {
						"name": "file_read",
						"arguments": null
					}
				}]
			},
			"finish_reason": "tool_calls"
		}],
		"usage": {
			"prompt_tokens": 1,
			"completion_tokens": 1,
			"total_tokens": 2
		}
	}`))

	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "tool call")
	assert.Contains(t, err.Error(), "file_read")
	assert.Contains(t, err.Error(), "arguments")
}

func TestClaudeProvider_ParseResponse_RejectsInvalidToolArguments(t *testing.T) {
	provider := NewClaudeProvider(ClaudeConfig{})

	_, err := provider.parseResponse([]byte(`{
		"content": [{
			"type": "tool_use",
			"id": "call-1",
			"name": "file_read",
			"input": null
		}],
		"usage": {
			"input_tokens": 1,
			"output_tokens": 1
		}
	}`))

	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "tool call")
	assert.Contains(t, err.Error(), "file_read")
	assert.Contains(t, err.Error(), "arguments")
}
