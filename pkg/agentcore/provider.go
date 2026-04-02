package agentcore

import "context"

// Provider generates the next model result from a prepared request.
type Provider interface {
	Name() string
	Generate(ctx context.Context, req *GenerateRequest) (*GenerateResult, error)
}

// GenerateRequest is the portable model request contract for agentcore runtimes.
type GenerateRequest struct {
	Model       string
	System      string
	Messages    []Message
	Tools       []ToolSchema
	MaxTokens   int
	Temperature float32
	Metadata    map[string]any
}

// GenerateResult is the portable model response contract for agentcore runtimes.
type GenerateResult struct {
	Content      string
	ToolCalls    []ToolCall
	FinishReason FinishReason
	Usage        Usage
	Artifacts    []Artifact
	Metadata     map[string]any
}

// FinishReason explains whether a model response completed the step or requested tools.
type FinishReason string

const (
	FinishFinal     FinishReason = "final"
	FinishToolCalls FinishReason = "tool_calls"
)

// Validate checks that the result is representable by the public runtime contract.
func (r *GenerateResult) Validate() error {
	if r == nil || (r.Content == "" && len(r.ToolCalls) == 0) {
		return ErrEmptyGenerateResult
	}
	if r.Content != "" && len(r.ToolCalls) > 0 {
		return ErrMixedGenerateResult
	}
	if r.FinishReason == "" {
		return ErrMissingFinishReason
	}
	for _, tc := range r.ToolCalls {
		if tc.CallID == "" {
			return ErrMissingToolCallID
		}
	}
	return nil
}
