package api

import "context"

// ErrorPolicy controls how the runtime reacts to recoverable execution errors.
//
// The default implementation preserves the current v0 runtime behavior:
// it does not override the classified error, leaving hooks or callers to decide
// whether to recover, yield, or fail.
type ErrorPolicy interface {
	HandleRecoverable(ctx context.Context, state *RunState, event *ErrorEvent) ErrorDecision
}

// ErrorDecision describes whether a recoverable runtime error should be turned
// into a replacement result or remain terminal.
type ErrorDecision struct {
	Result *RunResult
	Fail   error
}

// ResumePolicy controls how a runtime continues an existing run from persisted state.
//
// The default implementation preserves the current v0 resume semantics:
// terminal runs cannot be resumed, ResumeMode controls whether a new user input
// is appended, and input metadata overlays persisted metadata.
type ResumePolicy interface {
	AllowResume(ctx context.Context, input RunInput, state *RunState) error
	ShouldAppendInstruction(input RunInput, state *RunState) bool
	MergeMetadata(stateMetadata, inputMetadata map[string]any) map[string]any
}

// ToolExecutionPolicy controls validation and error handling around tool execution.
//
// The default implementation preserves the current v0 runtime behavior:
// tool-call batches can be limited per step, and tool execution failures are
// converted into tool-result transcript messages so the run can continue.
type ToolExecutionPolicy interface {
	ValidateToolCalls(ctx context.Context, state *RunState, calls []ToolCall) error
	HandleToolError(ctx context.Context, state *RunState, call ToolCall, err error) ToolErrorDecision
}

// ToolErrorDecision describes how the runtime should react to a tool execution failure.
type ToolErrorDecision struct {
	Result *ToolResult
	Fail   error
}
