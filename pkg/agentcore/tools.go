package agentcore

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/inhere/axon/pkg/agentcore/api"
)

type ToolCall = api.ToolCall
type ToolResult = api.ToolResult
type ToolSchema = api.ToolSchema
type Tool = api.Tool
type ToolRegistry = api.ToolRegistry

// Registry is the default in-memory implementation of ToolRegistry.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry creates an empty default tool registry.
func NewToolRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register adds or replaces a tool keyed by its name.
func (r *Registry) Register(tool Tool) {
	if r == nil || tool == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.tools == nil {
		r.tools = make(map[string]Tool)
	}
	r.tools[tool.Name()] = tool
}

// Schemas returns registered tool schemas in a deterministic order.
func (r *Registry) Schemas() []ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.tools) == 0 {
		return nil
	}

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)

	schemas := make([]ToolSchema, 0, len(names))
	for _, name := range names {
		schema := r.tools[name].Schema()
		if schema.Name == "" {
			schema.Name = name
		}
		if schema.Description == "" {
			schema.Description = r.tools[name].Description()
		}
		schemas = append(schemas, schema)
	}
	return schemas
}

// ListTools exposes registered schemas through the public registry contract.
func (r *Registry) ListTools(context.Context) ([]ToolSchema, error) {
	return r.Schemas(), nil
}

// ExecuteTool dispatches a tool call to the registered tool with the same name.
func (r *Registry) ExecuteTool(ctx context.Context, call ToolCall) (*ToolResult, error) {
	if r == nil {
		return nil, &ToolError{Err: fmt.Errorf("tool registry is nil")}
	}

	r.mu.RLock()
	tool, ok := r.tools[call.Name]
	r.mu.RUnlock()
	if !ok {
		return nil, &ToolError{Err: fmt.Errorf("tool %q not registered", call.Name)}
	}

	return tool.Execute(ctx, call)
}
