package agentcore

import "errors"

var (
	// ErrEmptyGenerateResult indicates the provider returned no content and no tool calls.
	ErrEmptyGenerateResult = errors.New("empty result")
	// ErrMixedGenerateResult indicates final content and tool calls were returned together.
	ErrMixedGenerateResult = errors.New("content and tool calls are mutually exclusive")
	// ErrMissingToolCallID indicates a requested tool call omitted its stable call identifier.
	ErrMissingToolCallID = errors.New("missing tool call id")
	// ErrMissingFinishReason indicates the provider omitted the result finish reason.
	ErrMissingFinishReason = errors.New("missing finish reason")
)
