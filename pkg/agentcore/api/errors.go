package api

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

// HistoryError classifies memory-layer runtime failures while preserving the wrapped cause.
type HistoryError struct {
	Err error
}

func (e *HistoryError) Error() string {
	if e == nil || e.Err == nil {
		return "memory error"
	}
	return "memory error: " + e.Err.Error()
}

func (e *HistoryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

var (
	ErrEmptyGenerateResult     = errors.New("empty result")
	ErrMixedGenerateResult     = errors.New("content and tool calls are mutually exclusive")
	ErrInvalidFinishReason     = errors.New("invalid finish reason for generate result payload")
	ErrMissingToolCallID       = errors.New("missing tool call id")
	ErrMissingToolName         = errors.New("missing tool name")
	ErrToolArgumentsInvalid    = errors.New("invalid tool call arguments")
	ErrMissingFinishReason     = errors.New("missing finish reason")
	ErrProviderRequired        = errors.New("provider is required")
	ErrRunNotResumable         = errors.New("run cannot be resumed from terminal state")
	ErrHistoryStateConflict    = errors.New("memory state conflicts with transcript")
	ErrToolCallsNotImplemented = errors.New("tool call loop not implemented yet")
	// ErrWrapUpIgnored: the reply to a wrap-up request (Control.RequestWrapUp)
	// called tools instead of answering.
	ErrWrapUpIgnored = errors.New("the model called tools after the wrap-up request")
)
