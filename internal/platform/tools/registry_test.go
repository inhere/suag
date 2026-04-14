package tools

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"

	"github.com/inhere/axon/internal/platform/llm"
	"github.com/inhere/axon/pkg/agentcore/api"
	agentcoretesting "github.com/inhere/axon/pkg/agentcore/testing"
)

// mockTool is a simple test double.
type mockTool struct {
	name   string
	result string
	err    error
}

func (m *mockTool) Name() string           { return m.name }
func (m *mockTool) Description() string    { return "mock tool" }
func (m *mockTool) Kind() Kind             { return KindNative }
func (m *mockTool) Schema() llm.ToolSchema { return llm.ToolSchema{Name: m.name} }
func (m *mockTool) Execute(_ context.Context, call api.ToolCall) (*api.ToolResult, error) {
	return &api.ToolResult{
		CallID:  call.CallID,
		Name:    m.name,
		Content: m.result,
	}, m.err
}

var _ api.ToolRegistry = (*Registry)(nil)

func TestRegistry_RegisterAndExecute(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockTool{name: "echo", result: "hello"})

	result, err := reg.ExecuteTool(context.Background(), api.ToolCall{Name: "echo"})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.Content != "hello" {
		t.Errorf("result = %q, want hello", result.Content)
	}
}

func TestRegistry_Execute_PassesArgumentsAndErrors(t *testing.T) {
	t.Run("passes arguments to registered tool", func(t *testing.T) {
		reg := NewRegistry()
		tool := &captureTool{name: "capture", result: "ok"}
		reg.Register(tool)

		result, err := reg.ExecuteTool(context.Background(), api.ToolCall{
			CallID:    "call-1",
			Name:      "capture",
			Arguments: map[string]any{"msg": "hello", "count": 2},
		})
		if err != nil {
			t.Fatalf("Execute error: %v", err)
		}
		if result.CallID != "call-1" || result.Name != "capture" || result.Content != "ok" {
			t.Fatalf("result = %#v, want preserved call metadata plus content", result)
		}
		if tool.lastArgs["msg"] != "hello" || tool.lastArgs["count"] != 2 {
			t.Fatalf("tool args = %#v, want msg/count to be preserved", tool.lastArgs)
		}
	})

	t.Run("returns underlying tool error", func(t *testing.T) {
		reg := NewRegistry()
		expectedErr := errors.New("boom")
		reg.Register(&captureTool{name: "broken", err: expectedErr})

		_, err := reg.ExecuteTool(context.Background(), api.ToolCall{
			Name:      "broken",
			Arguments: map[string]any{"msg": "fail"},
		})
		if !errors.Is(err, expectedErr) {
			t.Fatalf("err = %v, want %v", err, expectedErr)
		}
	})
}

func TestRegistry_DuplicatePanics(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockTool{name: "tool"})

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for duplicate tool name")
		}
	}()
	reg.Register(&mockTool{name: "tool"})
}

func TestRegistry_NotFound(t *testing.T) {
	reg := NewRegistry()
	_, err := reg.ExecuteTool(context.Background(), api.ToolCall{Name: "missing"})
	if err == nil {
		t.Error("expected error for missing tool")
	}
}

func TestRegistry_Schemas(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockTool{name: "a"})
	reg.Register(&mockTool{name: "b"})

	schemas, err := reg.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools error: %v", err)
	}
	if len(schemas) != 2 {
		t.Errorf("got %d schemas, want 2", len(schemas))
	}
}

func TestRegistry_Schemas_PreserveMetadataAndListStaysComplete(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&schemaTool{name: "search"})
	reg.Register(&schemaTool{name: "write_file"})

	schemas, err := reg.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools error: %v", err)
	}
	if len(schemas) != 2 {
		t.Fatalf("schemas = %d, want 2", len(schemas))
	}

	byName := map[string]api.ToolSchema{}
	for _, schema := range schemas {
		byName[schema.Name] = schema
	}

	searchSchema, ok := byName["search"]
	if !ok {
		t.Fatalf("schemas missing search: %#v", schemas)
	}
	if searchSchema.Description != "schema search" {
		t.Fatalf("search description = %q, want schema search", searchSchema.Description)
	}
	if searchSchema.InputSchema["type"] != "object" {
		t.Fatalf("search parameters type = %#v, want object", searchSchema.InputSchema["type"])
	}
	required, ok := searchSchema.InputSchema["required"].([]any)
	if !ok || len(required) != 1 || required[0] != "query" {
		t.Fatalf("search required = %#v, want query", searchSchema.InputSchema["required"])
	}
	properties, ok := searchSchema.InputSchema["properties"].(map[string]any)
	if !ok || properties["query"] == nil {
		t.Fatalf("search schema properties = %#v, want query property", searchSchema.InputSchema["properties"])
	}

	names := reg.List()
	sort.Strings(names)
	if len(names) != 2 || names[0] != "search" || names[1] != "write_file" {
		t.Fatalf("list = %#v, want [search write_file]", names)
	}
}

func TestRegistry_AgentcoreToolRegistryContract(t *testing.T) {
	agentcoretesting.RunToolRegistrySuite(t, func(t *testing.T) agentcoretesting.ToolRegistryFixture {
		reg := NewRegistry()
		reg.Register(&captureTool{name: "echo", result: "echo:hello"})

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

type captureTool struct {
	name     string
	result   string
	err      error
	lastArgs map[string]any
}

func (m *captureTool) Name() string           { return m.name }
func (m *captureTool) Description() string    { return "capture tool" }
func (m *captureTool) Kind() Kind             { return KindNative }
func (m *captureTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        m.name,
		Description: m.Description(),
		Parameters: llm.SchemaProperty{
			Type: "object",
			Properties: map[string]*llm.SchemaProperty{
				"msg": {Type: "string", Description: "message"},
			},
		},
	}
}
func (m *captureTool) Execute(_ context.Context, call api.ToolCall) (*api.ToolResult, error) {
	m.lastArgs = cloneArgs(call.Arguments)
	return &api.ToolResult{
		CallID:  call.CallID,
		Name:    call.Name,
		Content: m.result,
	}, m.err
}

type schemaTool struct {
	name string
}

func (m *schemaTool) Name() string        { return m.name }
func (m *schemaTool) Description() string { return "schema " + m.name }
func (m *schemaTool) Kind() Kind          { return KindNative }
func (m *schemaTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        m.name,
		Description: "schema " + m.name,
		Parameters: llm.SchemaProperty{
			Type: "object",
			Properties: map[string]*llm.SchemaProperty{
				"query": {Type: "string", Description: "query text"},
			},
			Required: []string{"query"},
		},
	}
}
func (m *schemaTool) Execute(_ context.Context, call api.ToolCall) (*api.ToolResult, error) {
	return &api.ToolResult{
		CallID:  call.CallID,
		Name:    call.Name,
		Content: fmt.Sprintf("schema:%s", m.name),
	}, nil
}

func cloneArgs(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
