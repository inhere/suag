package tools

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/inhere/axon/internal/platform/llm"
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
func (m *mockTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	return m.result, m.err
}

func TestRegistry_RegisterAndExecute(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockTool{name: "echo", result: "hello"})

	result, err := reg.Execute(context.Background(), "echo", nil)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result != "hello" {
		t.Errorf("result = %q, want hello", result)
	}
}

func TestRegistry_Execute_PassesArgumentsAndErrors(t *testing.T) {
	t.Run("passes arguments to registered tool", func(t *testing.T) {
		reg := NewRegistry()
		tool := &captureTool{name: "capture", result: "ok"}
		reg.Register(tool)

		args := map[string]any{"msg": "hello", "count": 2}
		result, err := reg.Execute(context.Background(), "capture", args)
		if err != nil {
			t.Fatalf("Execute error: %v", err)
		}
		if result != "ok" {
			t.Fatalf("result = %q, want ok", result)
		}
		if tool.lastArgs["msg"] != "hello" || tool.lastArgs["count"] != 2 {
			t.Fatalf("tool args = %#v, want msg/count to be preserved", tool.lastArgs)
		}
	})

	t.Run("returns underlying tool error", func(t *testing.T) {
		reg := NewRegistry()
		expectedErr := errors.New("boom")
		reg.Register(&captureTool{name: "broken", err: expectedErr})

		_, err := reg.Execute(context.Background(), "broken", map[string]any{"msg": "fail"})
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
	_, err := reg.Execute(context.Background(), "missing", nil)
	if err == nil {
		t.Error("expected error for missing tool")
	}
}

func TestRegistry_Schemas(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockTool{name: "a"})
	reg.Register(&mockTool{name: "b"})

	schemas := reg.Schemas()
	if len(schemas) != 2 {
		t.Errorf("got %d schemas, want 2", len(schemas))
	}
}

func TestRegistry_Schemas_PreserveMetadataAndListStaysComplete(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&schemaTool{name: "search"})
	reg.Register(&schemaTool{name: "write_file"})

	schemas := reg.Schemas()
	if len(schemas) != 2 {
		t.Fatalf("schemas = %d, want 2", len(schemas))
	}

	byName := map[string]llm.ToolSchema{}
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
	if searchSchema.Parameters.Type != "object" {
		t.Fatalf("search parameters type = %q, want object", searchSchema.Parameters.Type)
	}
	if len(searchSchema.Parameters.Required) != 1 || searchSchema.Parameters.Required[0] != "query" {
		t.Fatalf("search required = %#v, want query", searchSchema.Parameters.Required)
	}
	if searchSchema.Parameters.Properties["query"] == nil {
		t.Fatalf("search schema properties = %#v, want query property", searchSchema.Parameters.Properties)
	}

	names := reg.List()
	sort.Strings(names)
	if len(names) != 2 || names[0] != "search" || names[1] != "write_file" {
		t.Fatalf("list = %#v, want [search write_file]", names)
	}
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
func (m *captureTool) Schema() llm.ToolSchema { return llm.ToolSchema{Name: m.name} }
func (m *captureTool) Execute(_ context.Context, args map[string]any) (string, error) {
	m.lastArgs = cloneArgs(args)
	return m.result, m.err
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
func (m *schemaTool) Execute(context.Context, map[string]any) (string, error) {
	return m.name, nil
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
