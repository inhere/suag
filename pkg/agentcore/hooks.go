package agentcore

import (
	"context"
	"time"
)

// Hook defines the public lifecycle extension points for agentcore.
type Hook interface {
	BeforeRun(ctx context.Context, event RunEvent, ctrl *Control) error
	BeforePromptBuild(ctx context.Context, event PromptEvent, ctrl *Control) error
	BeforeContextBuild(ctx context.Context, event ContextEvent, ctrl *Control) error
	BeforeModel(ctx context.Context, event ModelEvent, ctrl *Control) error
	AfterModel(ctx context.Context, event ModelEvent, ctrl *Control) error
	BeforeTool(ctx context.Context, event ToolEvent, ctrl *Control) error
	AfterTool(ctx context.Context, event ToolEvent, ctrl *Control) error
	AfterRun(ctx context.Context, event RunEvent, ctrl *Control) error
	OnError(ctx context.Context, event ErrorEvent, ctrl *Control) error
}

// NopHook provides a no-op implementation for all public hook lifecycle methods.
type NopHook struct{}

func (NopHook) BeforeRun(context.Context, RunEvent, *Control) error              { return nil }
func (NopHook) BeforePromptBuild(context.Context, PromptEvent, *Control) error   { return nil }
func (NopHook) BeforeContextBuild(context.Context, ContextEvent, *Control) error { return nil }
func (NopHook) BeforeModel(context.Context, ModelEvent, *Control) error          { return nil }
func (NopHook) AfterModel(context.Context, ModelEvent, *Control) error           { return nil }
func (NopHook) BeforeTool(context.Context, ToolEvent, *Control) error            { return nil }
func (NopHook) AfterTool(context.Context, ToolEvent, *Control) error             { return nil }
func (NopHook) AfterRun(context.Context, RunEvent, *Control) error               { return nil }
func (NopHook) OnError(context.Context, ErrorEvent, *Control) error              { return nil }

// Control allows hooks to short-circuit or yield a run.
type Control struct {
	ShortCircuited bool
	Yielded        bool
	Replacement    any
	YieldReason    string
	YieldMetadata  map[string]any
}

// NewControl creates an empty lifecycle control object.
func NewControl() *Control {
	return &Control{}
}

// ShortCircuit stops normal processing and supplies a replacement value.
func (c *Control) ShortCircuit(replacement any) {
	if c == nil {
		return
	}
	c.ShortCircuited = true
	c.Replacement = replacement
}

// Yield requests that the runtime pause execution and surface yield metadata.
func (c *Control) Yield(reason string, metadata map[string]any) {
	if c == nil {
		return
	}
	c.Yielded = true
	c.YieldReason = reason
	c.YieldMetadata = cloneMetadata(metadata)
}

// ShouldYield reports whether a hook requested a cooperative yield.
func (c *Control) ShouldYield() bool {
	return c != nil && c.Yielded
}

// ShouldShortCircuit reports whether a hook replaced the normal flow.
func (c *Control) ShouldShortCircuit() bool {
	return c != nil && c.ShortCircuited
}

// RunEvent is the concrete lifecycle event for run boundaries.
type RunEvent struct {
	RunID     string
	AgentID   string
	Input     *RunInput
	Status    RunStatus
	Result    *GenerateResult
	Yield     *Yield
	Metadata  map[string]any
	Timestamp time.Time
}

// PromptEvent is the concrete lifecycle event for prompt construction.
type PromptEvent struct {
	RunID     string
	AgentID   string
	State     *RunState
	System    string
	Metadata  map[string]any
	Timestamp time.Time
}

// ContextEvent is the concrete lifecycle event for context construction.
type ContextEvent struct {
	RunID     string
	AgentID   string
	State     *RunState
	Messages  []Message
	Metadata  map[string]any
	Timestamp time.Time
}

// ModelEvent is the concrete lifecycle event for model calls and responses.
type ModelEvent struct {
	RunID     string
	AgentID   string
	Request   *GenerateRequest
	Result    *GenerateResult
	Metadata  map[string]any
	Timestamp time.Time
}

// ToolEvent is the concrete lifecycle event for tool execution.
type ToolEvent struct {
	RunID     string
	AgentID   string
	Call      *ToolCall
	Result    *ToolResult
	Metadata  map[string]any
	Timestamp time.Time
}

// ErrorEvent is the concrete lifecycle event for surfaced runtime errors.
type ErrorEvent struct {
	RunID     string
	AgentID   string
	Stage     string
	Err       error
	Metadata  map[string]any
	Timestamp time.Time
}

func cloneMetadata(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
