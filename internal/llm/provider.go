// Package llm defines the unified LLM provider interface and request/response types.
package llm

import "context"

// Provider is the unified interface for all LLM backends.
type Provider interface {
	// Complete performs a non-streaming completion.
	Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
	// Stream performs a streaming completion, returning tokens over a channel.
	Stream(ctx context.Context, req *CompletionRequest) (<-chan string, error)
	// Embed generates an embedding vector for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)
	// Name returns the provider name (e.g. "openai", "claude").
	Name() string
}

// Message is a single turn in a conversation.
type Message struct {
	Role      string     // "user" | "assistant" | "tool" | "system"
	Content   string
	ToolCall  *ToolCall  // set when role=="assistant" and LLM requested a tool
	ToolResult *ToolResult // set when role=="tool"
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

// CompletionRequest is the unified request payload sent to any LLM provider.
type CompletionRequest struct {
	Model       string
	Messages    []Message
	Tools       []ToolSchema
	MaxTokens   int
	Temperature float32
	System      string // optional system prompt override
}

// CompletionResponse is the unified response from any LLM provider.
type CompletionResponse struct {
	Content      string
	ToolCalls    []ToolCall
	Usage        TokenUsage
	FinishReason string // "stop" | "tool_calls" | "length"
}

// TokenUsage tracks token consumption for cost monitoring.
type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// IsToolCall returns true when the LLM response contains tool call requests.
func (r *CompletionResponse) IsToolCall() bool {
	return len(r.ToolCalls) > 0
}

// IsComplete returns true when the LLM indicated a natural stop.
func (r *CompletionResponse) IsComplete() bool {
	return r.FinishReason == "stop" || r.FinishReason == ""
}
