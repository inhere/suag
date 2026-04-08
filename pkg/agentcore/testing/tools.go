package agentcoretesting

import (
	"context"
	"reflect"
	stdtesting "testing"

	"github.com/inhere/axon/pkg/agentcore"
)

// ToolRegistryFixture configures a tool registry conformance scenario.
type ToolRegistryFixture struct {
	Registry       agentcore.ToolRegistry
	ExpectedSchema agentcore.ToolSchema
	ExecuteCall    agentcore.ToolCall
	WantResult     *agentcore.ToolResult
}

// NewToolRegistryFixture creates a minimal tool-registry conformance fixture.
func NewToolRegistryFixture(
	registry agentcore.ToolRegistry,
	schema agentcore.ToolSchema,
	call agentcore.ToolCall,
	want *agentcore.ToolResult,
) ToolRegistryFixture {
	return ToolRegistryFixture{
		Registry:       registry,
		ExpectedSchema: schema,
		ExecuteCall:    call,
		WantResult:     want,
	}
}

// ToolRegistryFactory returns a preconfigured registry fixture for one test case.
type ToolRegistryFactory func(t *stdtesting.T) ToolRegistryFixture

// RunToolRegistrySuite runs the minimal public tool registry conformance checks.
func RunToolRegistrySuite(t *stdtesting.T, factory ToolRegistryFactory) {
	t.Helper()
	if factory == nil {
		t.Fatal("agentcoretesting: tool registry factory is nil")
	}

	t.Run("list_tools_includes_expected_schema", func(t *stdtesting.T) {
		t.Helper()
		fixture := factory(t)
		if fixture.Registry == nil {
			t.Fatal("agentcoretesting: tool registry fixture returned nil registry")
		}

		schemas, err := fixture.Registry.ListTools(context.Background())
		if err != nil {
			t.Fatalf("expected ListTools to succeed, got %v", err)
		}

		found := false
		for _, schema := range schemas {
			if schema.Name != fixture.ExpectedSchema.Name {
				continue
			}
			found = true
			if schema.Description != fixture.ExpectedSchema.Description {
				t.Fatalf("expected tool description %q, got %#v", fixture.ExpectedSchema.Description, schema)
			}
			if !reflect.DeepEqual(schema.InputSchema, fixture.ExpectedSchema.InputSchema) {
				t.Fatalf("expected input schema to match, got %#v", schema.InputSchema)
			}
		}
		if !found {
			t.Fatalf("expected tool schema %q to be listed, got %#v", fixture.ExpectedSchema.Name, schemas)
		}
	})

	t.Run("execute_tool_returns_expected_result", func(t *stdtesting.T) {
		t.Helper()
		fixture := factory(t)
		if fixture.Registry == nil {
			t.Fatal("agentcoretesting: tool registry fixture returned nil registry")
		}

		got, err := fixture.Registry.ExecuteTool(context.Background(), fixture.ExecuteCall)
		if err != nil {
			t.Fatalf("expected ExecuteTool to succeed, got %v", err)
		}
		if !reflect.DeepEqual(got, fixture.WantResult) {
			t.Fatalf("expected tool result %#v, got %#v", fixture.WantResult, got)
		}
	})
}
