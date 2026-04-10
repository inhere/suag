package internalllm

import (
	"context"
	"fmt"

	"github.com/inhere/axon/internal/platform/llm"
	"github.com/inhere/axon/pkg/agentcore/api"
)

// Provider adapts an internal llm.Provider into the public agentcore api.Provider contract.
type Provider struct {
	inner llm.Provider
}

// NewProvider wraps an internal provider with the public agentcore api contract.
func NewProvider(inner llm.Provider) api.Provider {
	return &Provider{inner: inner}
}

// Name returns the underlying provider name.
func (p *Provider) Name() string {
	if p == nil || p.inner == nil {
		return ""
	}
	return p.inner.Name()
}

// Generate maps an agentcore request/response pair onto the internal llm provider.
func (p *Provider) Generate(ctx context.Context, req *api.GenerateRequest) (*api.GenerateResult, error) {
	if p == nil || p.inner == nil {
		return nil, &api.ProviderError{Err: fmt.Errorf("internal provider is nil")}
	}

	resp, err := p.inner.Complete(ctx, &llm.CompletionRequest{
		Model:       req.Model,
		Messages:    toInternalMessages(req.Messages),
		Tools:       toInternalTools(req.Tools),
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		System:      req.System,
	})
	if err != nil {
		return nil, &api.ProviderError{Err: err}
	}
	if resp == nil {
		return nil, &api.ProviderError{Err: fmt.Errorf("internal provider returned nil response")}
	}

	result := &api.GenerateResult{
		Content:   resp.Content,
		ToolCalls: toAgentcoreToolCalls(resp.ToolCalls),
		Usage: api.Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		},
	}

	switch resp.FinishReason {
	case "", "stop":
		result.FinishReason = api.FinishFinal
	case "tool_calls":
		result.FinishReason = api.FinishToolCalls
	case "length":
		return nil, &api.ProviderError{Err: fmt.Errorf("unsupported finish reason: %s", resp.FinishReason)}
	default:
		return nil, &api.ProviderError{Err: fmt.Errorf("unknown finish reason: %s", resp.FinishReason)}
	}

	if err := result.Validate(); err != nil {
		return nil, &api.ProviderError{Err: err}
	}
	return result, nil
}

func toInternalMessages(messages []api.Message) []llm.Message {
	if len(messages) == 0 {
		return nil
	}

	out := make([]llm.Message, len(messages))
	for i, msg := range messages {
		out[i] = llm.Message{
			Role:    msg.Role,
			Content: msg.Content,
		}
		if msg.ToolCall != nil {
			out[i].ToolCall = &llm.ToolCall{
				ID:        msg.ToolCall.CallID,
				Name:      msg.ToolCall.Name,
				Arguments: cloneMap(msg.ToolCall.Arguments),
			}
		}
		if msg.ToolResult != nil {
			callID := msg.ToolResult.CallID
			if callID == "" && msg.ToolCall != nil {
				callID = msg.ToolCall.CallID
			}
			out[i].ToolResult = &llm.ToolResult{
				ToolCallID: callID,
				Content:    msg.ToolResult.Content,
			}
		}
	}
	return out
}

func toInternalTools(tools []api.ToolSchema) []llm.ToolSchema {
	if len(tools) == 0 {
		return nil
	}

	out := make([]llm.ToolSchema, len(tools))
	for i, tool := range tools {
		out[i] = llm.ToolSchema{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  toSchemaProperty(tool.InputSchema),
		}
	}
	return out
}

func toAgentcoreToolCalls(calls []llm.ToolCall) []api.ToolCall {
	if len(calls) == 0 {
		return nil
	}

	out := make([]api.ToolCall, len(calls))
	for i, call := range calls {
		out[i] = api.ToolCall{
			CallID:    call.ID,
			Name:      call.Name,
			Arguments: cloneMap(call.Arguments),
		}
	}
	return out
}

func toSchemaProperty(schema map[string]any) llm.SchemaProperty {
	prop := llm.SchemaProperty{}
	if len(schema) == 0 {
		return prop
	}

	if value, ok := schema["type"].(string); ok {
		prop.Type = value
	}
	if value, ok := schema["description"].(string); ok {
		prop.Description = value
	}
	switch enum := schema["enum"].(type) {
	case []string:
		prop.Enum = append([]string(nil), enum...)
	case []any:
		prop.Enum = make([]string, 0, len(enum))
		for _, item := range enum {
			if text, ok := item.(string); ok {
				prop.Enum = append(prop.Enum, text)
			}
		}
	}
	switch required := schema["required"].(type) {
	case []string:
		prop.Required = append([]string(nil), required...)
	case []any:
		prop.Required = make([]string, 0, len(required))
		for _, item := range required {
			if text, ok := item.(string); ok {
				prop.Required = append(prop.Required, text)
			}
		}
	}
	if properties, ok := schema["properties"].(map[string]any); ok {
		prop.Properties = make(map[string]*llm.SchemaProperty, len(properties))
		for name, raw := range properties {
			if nested, ok := raw.(map[string]any); ok {
				child := toSchemaProperty(nested)
				prop.Properties[name] = &child
			}
		}
	}
	return prop
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}

	dst := make(map[string]any, len(src))
	for k, v := range src {
		switch typed := v.(type) {
		case map[string]any:
			dst[k] = cloneMap(typed)
		case []any:
			items := make([]any, len(typed))
			for i, item := range typed {
				if nested, ok := item.(map[string]any); ok {
					items[i] = cloneMap(nested)
					continue
				}
				items[i] = item
			}
			dst[k] = items
		default:
			dst[k] = v
		}
	}
	return dst
}
