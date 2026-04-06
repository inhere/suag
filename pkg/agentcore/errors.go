package agentcore

import "errors"

// ProviderError classifies provider-layer runtime failures while preserving the wrapped cause.
type ProviderError struct {
	Err error
}

func (e *ProviderError) Error() string {
	if e == nil || e.Err == nil {
		return "provider error"
	}
	return "provider error: " + e.Err.Error()
}

func (e *ProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ToolError classifies tool execution failures while preserving the wrapped cause.
type ToolError struct {
	Err error
}

func (e *ToolError) Error() string {
	if e == nil || e.Err == nil {
		return "tool error"
	}
	return "tool error: " + e.Err.Error()
}

func (e *ToolError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// HookError classifies hook lifecycle failures while preserving the wrapped cause.
type HookError struct {
	Err error
}

func (e *HookError) Error() string {
	if e == nil || e.Err == nil {
		return "hook error"
	}
	return "hook error: " + e.Err.Error()
}

func (e *HookError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// StorageError classifies storage-layer runtime failures while preserving the wrapped cause.
type StorageError struct {
	Err error
}

func (e *StorageError) Error() string {
	if e == nil || e.Err == nil {
		return "storage error"
	}
	return "storage error: " + e.Err.Error()
}

func (e *StorageError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// MemoryError classifies memory-layer runtime failures while preserving the wrapped cause.
type MemoryError struct {
	Err error
}

func (e *MemoryError) Error() string {
	if e == nil || e.Err == nil {
		return "memory error"
	}
	return "memory error: " + e.Err.Error()
}

func (e *MemoryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

var (
	// ErrEmptyGenerateResult indicates the provider returned no content and no tool calls.
	ErrEmptyGenerateResult = errors.New("empty result")
	// ErrMixedGenerateResult indicates final content and tool calls were returned together.
	ErrMixedGenerateResult = errors.New("content and tool calls are mutually exclusive")
	// ErrInvalidFinishReason indicates the finish reason does not match the returned payload shape.
	ErrInvalidFinishReason = errors.New("invalid finish reason for generate result payload")
	// ErrMissingToolCallID indicates a requested tool call omitted its stable call identifier.
	ErrMissingToolCallID = errors.New("missing tool call id")
	// ErrMissingToolName indicates a requested tool call omitted its dispatchable tool name.
	ErrMissingToolName = errors.New("missing tool name")
	// ErrMissingFinishReason indicates the provider omitted the result finish reason.
	ErrMissingFinishReason = errors.New("missing finish reason")
	// ErrProviderRequired indicates a runtime cannot execute without a configured provider.
	ErrProviderRequired = errors.New("provider is required")
	// ErrRunNotResumable indicates a terminal run cannot be continued.
	ErrRunNotResumable = errors.New("run cannot be resumed from terminal state")
	// ErrToolCallsNotImplemented indicates the runtime does not yet support tool-call loops.
	ErrToolCallsNotImplemented = errors.New("tool call loop not implemented yet")
)
