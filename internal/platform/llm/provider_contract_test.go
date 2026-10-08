package llm

import (
	"testing"

	"github.com/gookit/goutil/x/assert"
	agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"
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

func TestOpenAIProvider_ParseResponse_AcceptsStringifiedToolArguments(t *testing.T) {
	provider := NewOpenAIProvider(OpenAIConfig{})

	resp, err := provider.parseResponse([]byte(`{
		"choices": [{
			"message": {
				"tool_calls": [{
					"id": "call-1",
					"function": {
						"name": "file_list",
						"arguments": "{\"path\":\".\"}"
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

	assert.NoErr(t, err)
	assert.NotNil(t, resp)
	assert.Eq(t, FinishToolCalls, resp.FinishReason)
	assert.Eq(t, 1, len(resp.ToolCalls))
	assert.Eq(t, "file_list", resp.ToolCalls[0].Name)
	assert.Eq(t, ".", resp.ToolCalls[0].Arguments["path"])
}

func TestOpenAIProvider_ParseResponse_CapturesCacheUsage(t *testing.T) {
	provider := NewOpenAIProvider(OpenAIConfig{})
	resp, err := provider.parseResponse([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":3,"total_tokens":15,"prompt_tokens_details":{"cached_tokens":4},"cost_usd":0.01}}`))
	assert.NoErr(t, err)
	assert.Eq(t, 4, resp.Usage.CacheReadTokens)
	assert.Eq(t, 0.01, resp.Usage.CostUSD)
}

func TestClaudeProvider_ParseResponse_CapturesCacheUsage(t *testing.T) {
	provider := NewClaudeProvider(ClaudeConfig{})
	resp, err := provider.parseResponse([]byte(`{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":12,"output_tokens":3,"cache_read_input_tokens":4,"cache_creation_input_tokens":2}}`))
	assert.NoErr(t, err)
	assert.Eq(t, 4, resp.Usage.CacheReadTokens)
	assert.Eq(t, 2, resp.Usage.CacheWriteTokens)
	assert.Eq(t, 18, resp.Usage.InputTokens)
	assert.Eq(t, 21, resp.Usage.TotalTokens)
}

func TestOpenAIProvider_ParseResponse_PrefersToolCallsOverMixedContent(t *testing.T) {
	provider := NewOpenAIProvider(OpenAIConfig{})

	resp, err := provider.parseResponse([]byte(`{
		"choices": [{
			"message": {
				"content": "我先看一下目录。",
				"tool_calls": [{
					"id": "call-1",
					"function": {
						"name": "file_list",
						"arguments": "{\"path\":\".\"}"
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

	assert.NoErr(t, err)
	assert.NotNil(t, resp)
	assert.Eq(t, FinishToolCalls, resp.FinishReason)
	assert.Eq(t, "", resp.Content)
	assert.Eq(t, 1, len(resp.ToolCalls))
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

func TestClaudeProvider_ParseResponse_PrefersToolCallsOverMixedContent(t *testing.T) {
	provider := NewClaudeProvider(ClaudeConfig{})

	resp, err := provider.parseResponse([]byte(`{
		"content": [
			{
				"type": "text",
				"text": "我先看一下。"
			},
			{
				"type": "tool_use",
				"id": "call-1",
				"name": "file_list",
				"input": {"path":"."}
			}
		],
		"usage": {
			"input_tokens": 1,
			"output_tokens": 1
		}
	}`))

	assert.NoErr(t, err)
	assert.NotNil(t, resp)
	assert.Eq(t, FinishToolCalls, resp.FinishReason)
	assert.Eq(t, "", resp.Content)
	assert.Eq(t, 1, len(resp.ToolCalls))
	assert.Eq(t, "file_list", resp.ToolCalls[0].Name)
	assert.Eq(t, ".", resp.ToolCalls[0].Arguments["path"])
}

func TestClaudeProvider_BuildRequestBody_MovesSystemMessagesToSystemField(t *testing.T) {
	provider := NewClaudeProvider(ClaudeConfig{})
	body := provider.buildRequestBody(&agentcoreapi.GenerateRequest{
		System: "base",
		Messages: []agentcoreapi.Message{
			{Role: "user", Content: "a"},
			{Role: "system", Content: "项目规则已更新"},
		},
	}, "m")
	msgs := body["messages"].([]map[string]any)
	assert.Eq(t, 1, len(msgs))
	for _, m := range msgs {
		assert.Eq(t, "user", m["role"])
	}
	assert.Eq(t, "base\n\n项目规则已更新", body["system"])

	// Without a base system prompt the system message becomes the system field.
	body = provider.buildRequestBody(&agentcoreapi.GenerateRequest{
		Messages: []agentcoreapi.Message{{Role: "system", Content: "only"}, {Role: "user", Content: "a"}},
	}, "m")
	assert.Eq(t, "only", body["system"])
}
