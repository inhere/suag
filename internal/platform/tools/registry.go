// Package tools provides the tool registry and execution framework.
package tools

import (
	"context"
	"fmt"
	"sync"

	"github.com/inhere/axon/internal/platform/llm"
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
	Schema() llm.ToolSchema
	// Execute runs the tool with the given arguments.
	Execute(ctx context.Context, args map[string]any) (string, error)
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
	t, err := r.Get(name)
	if err != nil {
		return "", err
	}
	return t.Execute(ctx, args)
}

// Schemas returns the ToolSchema list for all registered tools, suitable for LLM function-calling.
func (r *Registry) Schemas() []llm.ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()
	schemas := make([]llm.ToolSchema, 0, len(r.tools))
	for _, t := range r.tools {
		schemas = append(schemas, t.Schema())
	}
	return schemas
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
