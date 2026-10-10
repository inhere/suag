package agentcoretesting

import (
	"context"
	"errors"
	"github.com/inhere/suag/pkg/agentcore/tools"
	stdtesting "testing"

	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/mock"
)

func TestRunToolRegistrySuite_WithDefaultRegistry(t *stdtesting.T) {
	execErr := errors.New("tool boom")

	RunToolRegistrySuite(t, func(t *stdtesting.T) ToolRegistryFixture {
		t.Helper()

		registry := tools.NewRegistry()
		registry.Register(mock.NewTool(
			"echo",
			mock.WithDescription("echo text"),
			mock.WithSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{"type": "string"},
				},
			}),
		))
		registry.Register(mock.NewTool(
			"fail",
			mock.WithHandler(func(_ context.Context, _ agentcore.ToolCall) (*agentcore.ToolResult, error) {
				return nil, execErr
			}),
		))

		return NewToolRegistryFixture(
			registry,
			agentcore.ToolSchema{
				Name:        "echo",
				Description: "echo text",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"text": map[string]any{"type": "string"},
					},
				},
			},
			agentcore.ToolCall{
				CallID:    "call-1",
				Name:      "echo",
				Arguments: map[string]any{"text": "hello"},
			},
			&agentcore.ToolResult{
				CallID:  "call-1",
				Name:    "echo",
				Content: "hello",
			},
		).WithExpectedSchema(agentcore.ToolSchema{
			Name:        "fail",
			Description: "mock tool fail",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": true,
			},
		}).WithExecuteError(
			"execute_error",
			agentcore.ToolCall{
				CallID:    "call-2",
				Name:      "fail",
				Arguments: map[string]any{"text": "boom"},
			},
			execErr,
		)
	})
}
