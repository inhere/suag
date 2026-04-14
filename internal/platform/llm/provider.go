// Package llm implements first-party model providers for Axon.
package llm

import (
	"context"

	agentcoreapi "github.com/inhere/axon/pkg/agentcore/api"
)

// Provider is the primary first-party LLM contract. It embeds the public
// agentcore provider surface while keeping a narrow legacy completion facade so
// existing internal callers can migrate gradually.
type Provider interface {
	agentcoreapi.Provider
	CompletionProvider
}

// StreamingProvider adds both the public streaming contract and the legacy
// text-channel streaming helper for migration callers.
type StreamingProvider interface {
	Provider
	agentcoreapi.StreamingProvider
	CompletionStreamingProvider
}

// EmbeddingProvider is the optional public embedding capability.
type EmbeddingProvider interface {
	Provider
	agentcoreapi.EmbeddingProvider
}

// Message is a single turn in a conversation.
type Message struct {
	Role       string
	Content    string
	ToolCall   *ToolCall
	ToolResult *ToolResult
}

// ToolCall represents a function call requested by the LLM.
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

// ToolResult is the outcome of a tool execution returned to the LLM.
type ToolResult struct {
	ToolCallID string
	Content    string
}

// ToolSchema describes a tool to the LLM in JSON Schema format.
type ToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  SchemaProperty `json:"parameters"`
}

// SchemaProperty is a JSON Schema property node.
type SchemaProperty struct {
	Type        string                     `json:"type"`
	Properties  map[string]*SchemaProperty `json:"properties,omitempty"`
	Required    []string                   `json:"required,omitempty"`
	Description string                     `json:"description,omitempty"`
	Enum        []string                   `json:"enum,omitempty"`
}

// CompletionRequest keeps the legacy request naming for internal callers.
type CompletionRequest struct {
	Model       string
	Messages    []Message
	Tools       []ToolSchema
	MaxTokens   int
	Temperature float32
	System      string
}

// CompletionResponse keeps the legacy response naming for internal callers.
type CompletionResponse struct {
	Content      string
	ToolCalls    []ToolCall
	Usage        TokenUsage
	FinishReason string
}

// TokenUsage tracks token consumption for cost monitoring.
type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// CompletionProvider preserves the completion-oriented facade that older
// internal callers still use while the runtime converges on agentcore/api.
type CompletionProvider interface {
	Name() string
	Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
}

// CompletionStreamingProvider preserves the channel-based text stream helper
// still used by migration callers such as ReactRunner-era helpers and pools.
type CompletionStreamingProvider interface {
	CompletionProvider
	StreamText(ctx context.Context, req *CompletionRequest) (<-chan string, error)
}

// IsToolCall returns true when the response contains tool call requests.
func (r *CompletionResponse) IsToolCall() bool {
	return r != nil && len(r.ToolCalls) > 0
}

// IsComplete returns true when the response indicates a natural final answer.
func (r *CompletionResponse) IsComplete() bool {
	return r != nil && (r.FinishReason == "stop" || r.FinishReason == "")
}

func toGenerateRequest(req *CompletionRequest) *agentcoreapi.GenerateRequest {
	if req == nil {
		return nil
	}

	out := &agentcoreapi.GenerateRequest{
		Model:       req.Model,
		System:      req.System,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}
	if len(req.Messages) > 0 {
		out.Messages = make([]agentcoreapi.Message, len(req.Messages))
		for i, msg := range req.Messages {
			out.Messages[i] = agentcoreapi.Message{
				Role:    msg.Role,
				Content: msg.Content,
			}
			if msg.ToolCall != nil {
				out.Messages[i].ToolCall = &agentcoreapi.ToolCall{
					CallID:    msg.ToolCall.ID,
					Name:      msg.ToolCall.Name,
					Arguments: cloneMap(msg.ToolCall.Arguments),
				}
			}
			if msg.ToolResult != nil {
				out.Messages[i].ToolResult = &agentcoreapi.ToolResult{
					CallID:  msg.ToolResult.ToolCallID,
					Content: msg.ToolResult.Content,
				}
			}
		}
	}
	if len(req.Tools) > 0 {
		out.Tools = make([]agentcoreapi.ToolSchema, len(req.Tools))
		for i, tool := range req.Tools {
			out.Tools[i] = agentcoreapi.ToolSchema{
				Name:        tool.Name,
				Description: tool.Description,
				InputSchema: schemaPropertyToMap(tool.Parameters),
			}
		}
	}
	return out
}

func fromGenerateResult(res *agentcoreapi.GenerateResult) *CompletionResponse {
	if res == nil {
		return nil
	}

	out := &CompletionResponse{
		Content: res.Content,
		Usage: TokenUsage{
			PromptTokens:     res.Usage.InputTokens,
			CompletionTokens: res.Usage.OutputTokens,
			TotalTokens:      res.Usage.TotalTokens,
		},
	}
	switch res.FinishReason {
	case agentcoreapi.FinishToolCalls:
		out.FinishReason = "tool_calls"
	case agentcoreapi.FinishFinal, "":
		out.FinishReason = "stop"
	default:
		out.FinishReason = string(res.FinishReason)
	}
	if len(res.ToolCalls) > 0 {
		out.ToolCalls = make([]ToolCall, len(res.ToolCalls))
		for i, call := range res.ToolCalls {
			out.ToolCalls[i] = ToolCall{
				ID:        call.CallID,
				Name:      call.Name,
				Arguments: cloneMap(call.Arguments),
			}
		}
	}
	return out
}

func schemaPropertyToMap(prop SchemaProperty) map[string]any {
	out := map[string]any{}
	if prop.Type != "" {
		out["type"] = prop.Type
	}
	if prop.Description != "" {
		out["description"] = prop.Description
	}
	if len(prop.Enum) > 0 {
		enum := make([]any, len(prop.Enum))
		for i, item := range prop.Enum {
			enum[i] = item
		}
		out["enum"] = enum
	}
	if len(prop.Required) > 0 {
		required := make([]any, len(prop.Required))
		for i, item := range prop.Required {
			required[i] = item
		}
		out["required"] = required
	}
	if len(prop.Properties) > 0 {
		props := make(map[string]any, len(prop.Properties))
		for name, child := range prop.Properties {
			if child == nil {
				continue
			}
			props[name] = schemaPropertyToMap(*child)
		}
		out["properties"] = props
	}
	return out
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
