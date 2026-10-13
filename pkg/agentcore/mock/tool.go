package mock

import (
	"context"
	"encoding/json"

	"github.com/inhere/suga/pkg/agentcore/api"
)

// ToolOption customizes the default mock tool.
type ToolOption func(*Tool)

// Tool is a deterministic test tool with overridable behavior.
type Tool struct {
	name        string
	description string
	schema      map[string]any
	handler     func(context.Context, api.ToolCall) (*api.ToolResult, error)
}

// NewTool creates a mock tool with a default echo-style handler.
func NewTool(name string, options ...ToolOption) *Tool {
	tool := &Tool{
		name:        name,
		description: "mock tool " + name,
		schema: map[string]any{
			"type":                 "object",
			"additionalProperties": true,
		},
	}
	for _, option := range options {
		if option != nil {
			option(tool)
		}
	}
	return tool
}

// WithDescription overrides the mock tool description.
func WithDescription(description string) ToolOption {
	return func(tool *Tool) {
		tool.description = description
	}
}

// WithSchema overrides the mock tool input schema.
func WithSchema(schema map[string]any) ToolOption {
	return func(tool *Tool) {
		tool.schema = cloneMap(schema)
	}
}

// WithHandler overrides the mock tool execution behavior.
func WithHandler(handler func(context.Context, api.ToolCall) (*api.ToolResult, error)) ToolOption {
	return func(tool *Tool) {
		tool.handler = handler
	}
}

// Name returns the dispatch name.
func (t *Tool) Name() string {
	return t.name
}

// Description returns the tool description.
func (t *Tool) Description() string {
	return t.description
}

// Schema returns a stable, provider-neutral tool schema.
func (t *Tool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.name,
		Description: t.description,
		InputSchema: cloneMap(t.schema),
	}
}

// Execute runs the configured handler or a default echo-style result.
func (t *Tool) Execute(ctx context.Context, call api.ToolCall) (*api.ToolResult, error) {
	if t.handler != nil {
		return t.handler(ctx, call)
	}

	return &api.ToolResult{
		CallID:  call.CallID,
		Name:    t.name,
		Content: defaultToolContent(call.Arguments),
	}, nil
}

func defaultToolContent(arguments map[string]any) string {
	if text, ok := arguments["text"].(string); ok {
		return text
	}
	if input, ok := arguments["input"].(string); ok {
		return input
	}
	if len(arguments) == 0 {
		return ""
	}

	data, err := json.Marshal(arguments)
	if err != nil {
		return ""
	}
	return string(data)
}
