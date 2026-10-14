package llm

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gookit/goutil/x/assert"

	agentcoreapi "github.com/inhere/suga/pkg/agentcore/api"
)

// toolChoiceTools is the declared tool set of the tool_choice cases: the field
// is only sent with tools (see buildRequestBody).
func toolChoiceTools() []agentcoreapi.ToolSchema {
	return []agentcoreapi.ToolSchema{{Name: "file_read", Description: "Read", InputSchema: map[string]any{"type": "object"}}}
}

// jsonBody marshals a request body, so each case compares what goes on the wire.
func jsonBody(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(body)
	assert.Require(t, assert.NoErr(t, err))
	var got map[string]any
	assert.Require(t, assert.NoErr(t, json.Unmarshal(raw, &got)))
	return got
}

// TestToolChoiceRequestBody covers the three request builders: the runtime's
// tool-less wrap-up retry sets GenerateRequest.ToolChoice, so each provider
// must turn it into the shape its API takes (a plain string for OpenAI and the
// Responses API, an object for Anthropic).
func TestToolChoiceRequestBody(t *testing.T) {
	req := func(toolChoice string) *agentcoreapi.GenerateRequest {
		return &agentcoreapi.GenerateRequest{
			Messages:   []agentcoreapi.Message{{Role: "user", Content: "go"}},
			Tools:      toolChoiceTools(),
			ToolChoice: toolChoice,
		}
	}

	t.Run("openai sends the string", func(t *testing.T) {
		p := NewOpenAIProvider(OpenAIConfig{})
		assert.Eq(t, "none", jsonBody(t, p.buildRequestBody(req(agentcoreapi.ToolChoiceNone), "m", false))["tool_choice"])
	})
	t.Run("openai sends no tool_choice by default", func(t *testing.T) {
		p := NewOpenAIProvider(OpenAIConfig{})
		_, has := jsonBody(t, p.buildRequestBody(req(""), "m", false))["tool_choice"]
		assert.False(t, has)
	})
	t.Run("openai without declared tools sends no tool_choice", func(t *testing.T) {
		p := NewOpenAIProvider(OpenAIConfig{})
		bare := req(agentcoreapi.ToolChoiceNone)
		bare.Tools = nil
		_, has := jsonBody(t, p.buildRequestBody(bare, "m", false))["tool_choice"]
		assert.False(t, has)
	})
	t.Run("anthropic sends the object form", func(t *testing.T) {
		p := NewClaudeProvider(ClaudeConfig{})
		assert.Eq(t, map[string]any{"type": "none"},
			jsonBody(t, p.buildRequestBody(req(agentcoreapi.ToolChoiceNone), "m"))["tool_choice"])
		assert.Eq(t, nil, jsonBody(t, p.buildRequestBody(req(""), "m"))["tool_choice"])
	})
	t.Run("responses sends the string", func(t *testing.T) {
		p := NewResponsesProvider(ResponsesConfig{})
		assert.Eq(t, "none",
			jsonBody(t, p.buildResponsesBody(context.Background(), req(agentcoreapi.ToolChoiceNone), false))["tool_choice"])
	})
	t.Run("a gateway without tool_choice gets none", func(t *testing.T) {
		// command-code's compat says the gateway has no usable tool_choice.
		p := NewResponsesProvider(ResponsesConfig{OpenAIConfig: OpenAIConfig{BaseURL: "https://api.commandcode.ai/provider/v1"}})
		_, has := jsonBody(t, p.buildResponsesBody(context.Background(), req(agentcoreapi.ToolChoiceNone), false))["tool_choice"]
		assert.False(t, has)
	})
}

// noChoiceProvider is a provider that does not implement ToolChooser.
type noChoiceProvider struct{}

func (noChoiceProvider) Name() string { return "no-choice" }

func (noChoiceProvider) Generate(context.Context, *agentcoreapi.GenerateRequest) (*agentcoreapi.GenerateResult, error) {
	return &agentcoreapi.GenerateResult{Content: "hi", FinishReason: agentcoreapi.FinishFinal}, nil
}

// TestSupportsToolChoice covers what the runtime asks before it sends the
// tool-less retry: a provider (or pool) that does not take the field must not
// claim it.
func TestSupportsToolChoice(t *testing.T) {
	t.Run("the first-party providers take it", func(t *testing.T) {
		assert.True(t, NewOpenAIProvider(OpenAIConfig{}).SupportsToolChoice())
		assert.True(t, NewClaudeProvider(ClaudeConfig{}).SupportsToolChoice())
		assert.True(t, NewResponsesProvider(ResponsesConfig{}).SupportsToolChoice())
	})
	t.Run("a gateway with NoToolChoice does not", func(t *testing.T) {
		p := NewResponsesProvider(ResponsesConfig{OpenAIConfig: OpenAIConfig{BaseURL: "https://api.commandcode.ai/provider/v1"}})
		assert.False(t, p.SupportsToolChoice())
	})
	t.Run("a pool takes it only when every provider does", func(t *testing.T) {
		openai := NewOpenAIProvider(OpenAIConfig{})
		claude := NewClaudeProvider(ClaudeConfig{})
		assert.True(t, NewPool([]Provider{openai, claude}).SupportsToolChoice())
		assert.False(t, NewPool([]Provider{openai, noChoiceProvider{}}).SupportsToolChoice())
		assert.False(t, NewPool(nil).SupportsToolChoice())
	})
}
