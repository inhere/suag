package agentcore

import "context"

// Provider generates the next model result from a prepared request.
type Provider interface {
	Name() string
	Generate(ctx context.Context, req *GenerateRequest) (*GenerateResult, error)
}

// StreamEventKind identifies the public streaming event shape emitted by a provider.
type StreamEventKind string

const (
	StreamEventTextDelta   StreamEventKind = "text_delta"
	StreamEventMessageStop StreamEventKind = "message_stop"
)

// StreamEvent is the minimal host-facing streaming event contract.
type StreamEvent struct {
	Kind     StreamEventKind
	Delta    string
	Metadata map[string]any
}

// StreamFunc consumes a model streaming event.
type StreamFunc func(ctx context.Context, event StreamEvent) error

// StreamingProvider is an optional provider capability for incremental output delivery.
type StreamingProvider interface {
	Provider
	Stream(ctx context.Context, req *GenerateRequest, emit StreamFunc) (*GenerateResult, error)
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
	if r.Content != "" && r.FinishReason != FinishFinal {
		return ErrInvalidFinishReason
	}
	if len(r.ToolCalls) > 0 && r.FinishReason != FinishToolCalls {
		return ErrInvalidFinishReason
	}
	for _, tc := range r.ToolCalls {
		if tc.CallID == "" {
			return ErrMissingToolCallID
		}
		if tc.Name == "" {
			return ErrMissingToolName
		}
	}
	return nil
}
