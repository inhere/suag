package internaltools

import (
	"context"
	"fmt"

	internaltools "github.com/inhere/axon/internal/platform/tools"
	"github.com/inhere/axon/pkg/agentcore/api"
)

// Registry adapts an internal tools.Registry into the public agentcore api.ToolRegistry contract.
type Registry struct {
	inner *internaltools.Registry
}

// NewRegistry wraps an internal tool registry with the public agentcore api contract.
func NewRegistry(inner *internaltools.Registry) api.ToolRegistry {
	return &Registry{inner: inner}
}

// ListTools returns public tool schemas converted from the internal registry.
func (r *Registry) ListTools(context.Context) ([]api.ToolSchema, error) {
	if r == nil || r.inner == nil {
		return nil, nil
	}
	return r.inner.ListTools(context.Background())
}

// ExecuteTool dispatches a public tool call through the internal registry.
func (r *Registry) ExecuteTool(ctx context.Context, call api.ToolCall) (*api.ToolResult, error) {
	if r == nil || r.inner == nil {
		return nil, &api.ToolError{Err: fmt.Errorf("internal tool registry is nil")}
	}

	result, err := r.inner.ExecuteTool(ctx, call)
	if err != nil {
		return nil, &api.ToolError{Err: err}
	}
	return result, nil
}
