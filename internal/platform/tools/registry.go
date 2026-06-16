// Package tools provides the tool registry and execution framework.
package tools

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/inhere/suag/pkg/agentcore/api"
)

// Kind classifies the runtime type of a tool.
type Kind string

const (
	KindNative   Kind = "native"
	KindMCP      Kind = "mcp"
	KindSkill    Kind = "skill"
	KindBrowser  Kind = "browser"
	KindExtAgent Kind = "ext_agent"
)

// Tool is the interface every tool must implement.
type Tool interface {
	// Name returns the unique tool identifier used in LLM function calls.
	Name() string
	// Description returns a short human/LLM readable description.
	Description() string
	// Schema returns the JSON Schema for the tool's parameters.
	Schema() api.ToolSchema
	// Execute runs the tool with the given arguments.
	Execute(ctx context.Context, call api.ToolCall) (*api.ToolResult, error)
	// Kind returns the tool's runtime kind.
	Kind() Kind
}

// Registry holds all registered tools and dispatches calls.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool to the registry. Panics on duplicate name.
func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[t.Name()]; exists {
		panic(fmt.Sprintf("tool %q already registered", t.Name()))
	}
	r.tools[t.Name()] = t
}

// MustRegister is like Register but does not panic — it overwrites.
func (r *Registry) MustRegister(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

// Get returns the tool with the given name, or an error if not found.
func (r *Registry) Get(name string) (Tool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool %q not found", name)
	}
	return t, nil
}

// Execute dispatches a tool call by name.
func (r *Registry) Execute(ctx context.Context, name string, args map[string]any) (string, error) {
	result, err := r.ExecuteTool(ctx, api.ToolCall{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", nil
	}
	return result.Content, nil
}

// Schemas returns the ToolSchema list for all registered tools in deterministic order.
func (r *Registry) Schemas() []api.ToolSchema {
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

	schemas := make([]api.ToolSchema, 0, len(names))
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

// ListTools exposes registered schemas through the agentcore tool-registry contract.
func (r *Registry) ListTools(context.Context) ([]api.ToolSchema, error) {
	return r.Schemas(), nil
}

// ExecuteTool dispatches a public tool call to a registered tool.
func (r *Registry) ExecuteTool(ctx context.Context, call api.ToolCall) (*api.ToolResult, error) {
	t, err := r.Get(call.Name)
	if err != nil {
		return nil, err
	}

	result, err := t.Execute(ctx, cloneToolCall(call))
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = &api.ToolResult{}
	}
	if result.CallID == "" {
		result.CallID = call.CallID
	}
	if result.Name == "" {
		result.Name = call.Name
	}
	return result, nil
}

// List returns the names of all registered tools.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// Subset returns a new registry containing only the named tools that exist.
func (r *Registry) Subset(names []string) *Registry {
	subset := NewRegistry()
	if len(names) == 0 {
		return subset
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, name := range names {
		tool, ok := r.tools[name]
		if !ok {
			continue
		}
		subset.tools[name] = tool
	}
	return subset
}

func cloneToolCall(call api.ToolCall) api.ToolCall {
	return api.ToolCall{
		CallID:    call.CallID,
		Name:      call.Name,
		Arguments: cloneMap(call.Arguments),
	}
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
