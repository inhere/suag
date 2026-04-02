package agentcore

import "context"

// Prompt captures the assembled prompt sent to a provider.
type Prompt struct {
	System   string
	Messages []Message
	Tools    []ToolSchema
	Metadata map[string]any
}

// PromptInput captures the inputs required to assemble a prompt.
type PromptInput struct {
	Run      RunInput
	Context  []Message
	Tools    []ToolSchema
	Metadata map[string]any
}

// PromptBuilder builds a provider-ready prompt from public contracts.
type PromptBuilder interface {
	BuildPrompt(ctx context.Context, input PromptInput) (*Prompt, error)
}
