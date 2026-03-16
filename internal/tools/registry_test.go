package tools

import (
	"context"
	"testing"

	"github.com/kagent/kagent/internal/llm"
)

// mockTool is a simple test double.
type mockTool struct {
	name   string
	result string
	err    error
}

func (m *mockTool) Name() string             { return m.name }
func (m *mockTool) Description() string      { return "mock tool" }
func (m *mockTool) Kind() Kind               { return KindNative }
func (m *mockTool) Schema() llm.ToolSchema   { return llm.ToolSchema{Name: m.name} }
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
