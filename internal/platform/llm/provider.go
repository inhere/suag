// Package llm implements first-party model providers for Axon.
package llm

import agentcoreapi "github.com/inhere/axon/pkg/agentcore/api"

type Provider = agentcoreapi.Provider
type StreamingProvider = agentcoreapi.StreamingProvider
type EmbeddingProvider = agentcoreapi.EmbeddingProvider
type GenerateRequest = agentcoreapi.GenerateRequest
type GenerateResult = agentcoreapi.GenerateResult
type FinishReason = agentcoreapi.FinishReason

const (
	FinishFinal     = agentcoreapi.FinishFinal
	FinishToolCalls = agentcoreapi.FinishToolCalls
)

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

// TokenUsage tracks token consumption for cost monitoring.
type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
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
