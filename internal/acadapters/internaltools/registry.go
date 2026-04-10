package internaltools

import (
	"context"
	"fmt"
	"sort"

	"github.com/inhere/axon/internal/platform/llm"
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

	schemas := r.inner.Schemas()
	out := make([]api.ToolSchema, len(schemas))
	for i, schema := range schemas {
		out[i] = api.ToolSchema{
			Name:        schema.Name,
			Description: schema.Description,
			InputSchema: fromSchemaProperty(schema.Parameters),
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// ExecuteTool dispatches a public tool call through the internal registry.
func (r *Registry) ExecuteTool(ctx context.Context, call api.ToolCall) (*api.ToolResult, error) {
	if r == nil || r.inner == nil {
		return nil, &api.ToolError{Err: fmt.Errorf("internal tool registry is nil")}
	}

	content, err := r.inner.Execute(ctx, call.Name, cloneMap(call.Arguments))
	if err != nil {
		return nil, &api.ToolError{Err: err}
	}

	return &api.ToolResult{
		CallID:  call.CallID,
		Name:    call.Name,
		Content: content,
	}, nil
}

func fromSchemaProperty(prop llm.SchemaProperty) map[string]any {
	out := map[string]any{}
	if prop.Type != "" {
		out["type"] = prop.Type
	}
	if prop.Description != "" {
		out["description"] = prop.Description
	}
	if len(prop.Enum) > 0 {
		enum := make([]any, len(prop.Enum))
		for i, item := range prop.Enum {
			enum[i] = item
		}
		out["enum"] = enum
	}
	if len(prop.Required) > 0 {
		required := make([]any, len(prop.Required))
		for i, item := range prop.Required {
			required[i] = item
		}
		out["required"] = required
	}
	if len(prop.Properties) > 0 {
		properties := make(map[string]any, len(prop.Properties))
		for name, child := range prop.Properties {
			if child == nil {
				continue
			}
			properties[name] = fromSchemaProperty(*child)
		}
		out["properties"] = properties
	}
	return out
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
