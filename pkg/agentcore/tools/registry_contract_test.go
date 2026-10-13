package tools_test

// The agentcore registry conformance suite lives in agentcore/testing, which
// imports this package, so the contract test is an external test package.

import (
	"context"
	"testing"

	"github.com/inhere/suga/pkg/agentcore/api"
	agentcoretesting "github.com/inhere/suga/pkg/agentcore/testing"
	"github.com/inhere/suga/pkg/agentcore/tools"
)

func TestRegistry_AgentcoreToolRegistryContract(t *testing.T) {
	agentcoretesting.RunToolRegistrySuite(t, func(t *testing.T) agentcoretesting.ToolRegistryFixture {
		reg := tools.NewRegistry()
		reg.Register(&contractTool{name: "echo", result: "echo:hello"})

		return agentcoretesting.NewToolRegistryFixture(
			reg,
			api.ToolSchema{
				Name:        "echo",
				Description: "capture tool",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"msg": map[string]any{
							"type":        "string",
							"description": "message",
						},
					},
				},
			},
			api.ToolCall{
				CallID:    "call-echo",
				Name:      "echo",
				Arguments: map[string]any{"msg": "hello"},
			},
			&api.ToolResult{
				CallID:  "call-echo",
				Name:    "echo",
				Content: "echo:hello",
			},
		)
	})
}

// contractTool echoes a fixed result for the conformance suite.
type contractTool struct {
	name   string
	result string
}

func (m *contractTool) Name() string        { return m.name }
func (m *contractTool) Description() string { return "capture tool" }
func (m *contractTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        m.name,
		Description: m.Description(),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"msg": map[string]any{
					"type":        "string",
					"description": "message",
				},
			},
		},
	}
}

func (m *contractTool) Execute(_ context.Context, call api.ToolCall) (*api.ToolResult, error) {
	return &api.ToolResult{CallID: call.CallID, Name: call.Name, Content: m.result}, nil
}
