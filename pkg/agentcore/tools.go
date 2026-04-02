package agentcore

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// ToolCall is a model-requested invocation of a named tool.
type ToolCall struct {
	CallID    string
	Name      string
	Arguments map[string]any
}

// ToolResult is the public result returned after executing a tool call.
type ToolResult struct {
	CallID    string
	Name      string
	Content   string
	Artifacts []Artifact
	Metadata  map[string]any
}

// ToolSchema describes a callable tool in a provider-neutral way.
type ToolSchema struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// Tool exposes a concrete executable tool.
type Tool interface {
	Name() string
	Description() string
	Schema() ToolSchema
	Execute(ctx context.Context, call ToolCall) (*ToolResult, error)
}

// ToolRegistry exposes lookup and execution contracts for tools.
type ToolRegistry interface {
	ListTools(ctx context.Context) ([]ToolSchema, error)
	ExecuteTool(ctx context.Context, call ToolCall) (*ToolResult, error)
}

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
