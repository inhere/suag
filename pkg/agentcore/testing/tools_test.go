package agentcoretesting

import (
	stdtesting "testing"

	"github.com/inhere/axon/pkg/agentcore"
	"github.com/inhere/axon/pkg/agentcore/mock"
)

func TestRunToolRegistrySuite_WithDefaultRegistry(t *stdtesting.T) {
	RunToolRegistrySuite(t, func(t *stdtesting.T) ToolRegistryFixture {
		t.Helper()

		registry := agentcore.NewToolRegistry()
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
		)
	})
}
