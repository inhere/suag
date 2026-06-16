package agentcoretesting

import (
	"context"
	"errors"
	"reflect"
	stdtesting "testing"

	"github.com/inhere/suag/pkg/agentcore"
)

// ToolRegistryFixture configures a tool registry conformance scenario.
type ToolRegistryFixture struct {
	Registry        agentcore.ToolRegistry
	ExpectedSchema  agentcore.ToolSchema
	ExpectedSchemas []agentcore.ToolSchema
	ExecuteCall     agentcore.ToolCall
	WantResult      *agentcore.ToolResult
	WantErr         error
	ExecuteCases    []ToolExecuteCase
}

// ToolExecuteCase describes one expected ExecuteTool call against a registry.
type ToolExecuteCase struct {
	Name       string
	Call       agentcore.ToolCall
	WantResult *agentcore.ToolResult
	WantErr    error
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

// NewToolExecuteCase creates a named tool execution case.
func NewToolExecuteCase(name string, call agentcore.ToolCall, want *agentcore.ToolResult) ToolExecuteCase {
	return ToolExecuteCase{
		Name:       name,
		Call:       call,
		WantResult: want,
	}
}

// WithExpectedSchema appends one additional listed-schema expectation.
func (f ToolRegistryFixture) WithExpectedSchema(schema agentcore.ToolSchema) ToolRegistryFixture {
	f.ExpectedSchemas = append(f.ExpectedSchemas, schema)
	return f
}

// WithExecuteCase appends one additional execute success case.
func (f ToolRegistryFixture) WithExecuteCase(tc ToolExecuteCase) ToolRegistryFixture {
	f.ExecuteCases = append(f.ExecuteCases, tc)
	return f
}

// WithExecuteError appends one expected execute error case.
func (f ToolRegistryFixture) WithExecuteError(name string, call agentcore.ToolCall, wantErr error) ToolRegistryFixture {
	f.ExecuteCases = append(f.ExecuteCases, ToolExecuteCase{
		Name:    name,
		Call:    call,
		WantErr: wantErr,
	})
	return f
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

		expectedSchemas := fixture.listSchemaCases()
		if len(expectedSchemas) == 0 {
			t.Fatal("agentcoretesting: tool registry fixture returned no expected schemas")
		}
		for _, expected := range expectedSchemas {
			found := false
			for _, schema := range schemas {
				if schema.Name != expected.Name {
					continue
				}
				found = true
				if schema.Description != expected.Description {
					t.Fatalf("expected tool description %q, got %#v", expected.Description, schema)
				}
				if !reflect.DeepEqual(schema.InputSchema, expected.InputSchema) {
					t.Fatalf("expected input schema to match, got %#v", schema.InputSchema)
				}
			}
			if !found {
				t.Fatalf("expected tool schema %q to be listed, got %#v", expected.Name, schemas)
			}
		}
	})

	t.Run("execute_cases_match_expected_contract", func(t *stdtesting.T) {
		t.Helper()
		fixture := factory(t)
		if fixture.Registry == nil {
			t.Fatal("agentcoretesting: tool registry fixture returned nil registry")
		}

		executeCases := fixture.executeToolCases()
		if len(executeCases) == 0 {
			t.Fatal("agentcoretesting: tool registry fixture returned no execute cases")
		}

		for _, tc := range executeCases {
			name := tc.Name
			if name == "" {
				name = "execute"
			}
			t.Run(name, func(t *stdtesting.T) {
				got, err := fixture.Registry.ExecuteTool(context.Background(), tc.Call)
				if tc.WantErr != nil {
					if !errors.Is(err, tc.WantErr) {
						t.Fatalf("expected ExecuteTool error %v, got result=%#v err=%v", tc.WantErr, got, err)
					}
					return
				}
				if err != nil {
					t.Fatalf("expected ExecuteTool to succeed, got %v", err)
				}
				if !reflect.DeepEqual(got, tc.WantResult) {
					t.Fatalf("expected tool result %#v, got %#v", tc.WantResult, got)
				}
			})
		}
	})
}

func (f ToolRegistryFixture) listSchemaCases() []agentcore.ToolSchema {
	if len(f.ExpectedSchemas) > 0 {
		return append([]agentcore.ToolSchema(nil), f.ExpectedSchemas...)
	}
	if f.ExpectedSchema.Name == "" {
		return nil
	}
	return []agentcore.ToolSchema{f.ExpectedSchema}
}

func (f ToolRegistryFixture) executeToolCases() []ToolExecuteCase {
	if len(f.ExecuteCases) > 0 {
		return append([]ToolExecuteCase(nil), f.ExecuteCases...)
	}
	if f.ExecuteCall.Name == "" && f.ExecuteCall.CallID == "" {
		return nil
	}
	return []ToolExecuteCase{{
		Call:       f.ExecuteCall,
		WantResult: f.WantResult,
		WantErr:    f.WantErr,
	}}
}
