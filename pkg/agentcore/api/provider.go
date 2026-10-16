package api

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
	// StreamEventRetry announces that the request failed for a transient
	// reason and is retried after a wait: Delta is the reason, Metadata has
	// "attempt", "wait_ms" and "restart" (true when text of the failed
	// attempt was already emitted; the message streams again from its start).
	StreamEventRetry StreamEventKind = "retry"
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
	// ToolChoice asks the provider how freely the model may use the declared
	// tools; empty leaves the provider's default. ToolChoiceNone forbids tool
	// calls, which the runtime uses when a wrap-up step keeps asking for tools.
	// Only providers implementing ToolChooser honour it.
	ToolChoice string
}

// ToolChoiceNone is the ToolChoice value that forbids tool calls (OpenAI
// "none", Anthropic/Responses {"type": "none"}).
const ToolChoiceNone = "none"

// UsageEstimatedMetaKey marks GenerateResult usage that was filled or replaced
// by the personal-layer estimator. The metadata travels with the result so the
// runtime and observation layers keep the same estimated/real distinction.
const UsageEstimatedMetaKey = "usage_estimated"

// EstimatedInputTokensMetaKey and EstimatedOutputTokensMetaKey retain the
// estimator's two values for observers that need to show the estimate beside
// the normalized Usage fields.
const (
	EstimatedInputTokensMetaKey  = "est_input_tokens"
	EstimatedOutputTokensMetaKey = "est_output_tokens"
)

// ToolChooser is an optional provider capability: the provider honours
// GenerateRequest.ToolChoice. A provider without it is never asked for a reply
// with tool_choice (the runtime skips the tool-less wrap-up retry instead of
// risking a request the API rejects).
type ToolChooser interface {
	// SupportsToolChoice reports whether requests may carry a tool_choice.
	SupportsToolChoice() bool
}

// GenerateResult is the portable model response contract for agentcore runtimes.
type GenerateResult struct {
	Content      string
	ToolCalls    []ToolCall
	FinishReason FinishReason
	Usage        Usage
	Artifacts    []Artifact
	Metadata     map[string]any
	// Reasoning is the model's reasoning text returned with this result; the
	// runtime stores it on the assistant message it persists.
	Reasoning string
	// Truncated is true when the reply was cut off at the output token limit of
	// the request (OpenAI finish_reason "length", Claude stop_reason
	// "max_tokens", a Responses result with incomplete reason
	// "max_output_tokens"): the answer may be incomplete.
	Truncated bool
	// OutputLimit is how many output tokens the request allowed (the provider's
	// own default included); 0 when neither the request nor the protocol set
	// one, so the server's default applied. It names the limit in the
	// truncation notice.
	OutputLimit int
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
	// Content alongside ToolCalls is allowed: it is the model's text preceding the
	// tool requests (FinishToolCalls) and is persisted with the first tool-call message.
	if r.FinishReason == "" {
		return ErrMissingFinishReason
	}
	if r.Content != "" && len(r.ToolCalls) == 0 && r.FinishReason != FinishFinal {
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
