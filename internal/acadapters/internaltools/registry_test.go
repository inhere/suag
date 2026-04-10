package internaltools_test

import (
	"context"
	"testing"

	"github.com/inhere/axon/internal/acadapters/internaltools"
	"github.com/inhere/axon/internal/platform/llm"
	"github.com/inhere/axon/internal/platform/tools"
	"github.com/inhere/axon/pkg/agentcore/api"
)

type echoTool struct {
	lastArgs map[string]any
}

func (t *echoTool) Name() string        { return "echo" }
func (t *echoTool) Description() string { return "Echoes a message" }
func (t *echoTool) Kind() tools.Kind    { return tools.KindNative }
func (t *echoTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        "echo",
		Description: "Echoes a message",
		Parameters: llm.SchemaProperty{
			Type: "object",
			Properties: map[string]*llm.SchemaProperty{
				"text": {Type: "string", Description: "text to echo", Enum: []string{"hello", "world"}},
			},
			Required: []string{"text"},
		},
	}
}
func (t *echoTool) Execute(_ context.Context, args map[string]any) (string, error) {
	t.lastArgs = cloneArgs(args)
	return "echo:" + args["text"].(string), nil
}

func TestInternalToolsAdapter_ListsSchemasAndExecutes(t *testing.T) {
	reg := tools.NewRegistry()
	tool := &echoTool{}
	reg.Register(tool)

	adapter := internaltools.NewRegistry(reg)

	schemas, err := adapter.ListTools(context.Background())
	if err != nil {
		t.Fatalf("expected list tools to succeed, got %v", err)
	}
	if len(schemas) != 1 {
		t.Fatalf("schemas = %d, want 1", len(schemas))
	}
	if schemas[0].Name != "echo" || schemas[0].Description != "Echoes a message" {
		t.Fatalf("unexpected schema metadata: %#v", schemas[0])
	}
	if schemas[0].InputSchema["type"] != "object" {
		t.Fatalf("unexpected schema type: %#v", schemas[0].InputSchema)
	}
	properties, ok := schemas[0].InputSchema["properties"].(map[string]any)
	if !ok || properties["text"] == nil {
		t.Fatalf("expected text property in schema, got %#v", schemas[0].InputSchema)
	}
	textProp, ok := properties["text"].(map[string]any)
	if !ok {
		t.Fatalf("expected text property schema map, got %#v", properties["text"])
	}
	enum, ok := textProp["enum"].([]any)
	if !ok || len(enum) != 2 || enum[0] != "hello" || enum[1] != "world" {
		t.Fatalf("expected enum constraint to be preserved, got %#v", textProp)
	}

	result, err := adapter.ExecuteTool(context.Background(), api.ToolCall{
		CallID: "call-1",
		Name:   "echo",
		Arguments: map[string]any{
			"text": "hello",
		},
	})
	if err != nil {
		t.Fatalf("expected execute tool to succeed, got %v", err)
	}
	if result.CallID != "call-1" || result.Name != "echo" {
		t.Fatalf("expected call metadata preserved, got %#v", result)
	}
	if result.Content != "echo:hello" {
		t.Fatalf("content = %q, want echo:hello", result.Content)
	}
	if tool.lastArgs["text"] != "hello" {
		t.Fatalf("tool received args %#v, want text=hello", tool.lastArgs)
	}
}

func TestInternalToolsAdapter_ListTools_IsStableAndSortedByName(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&namedEchoTool{name: "z-last"})
	reg.Register(&namedEchoTool{name: "a-first"})
	reg.Register(&namedEchoTool{name: "m-middle"})

	adapter := internaltools.NewRegistry(reg)

	for i := 0; i < 200; i++ {
		schemas, err := adapter.ListTools(context.Background())
		if err != nil {
			t.Fatalf("expected list tools to succeed, got %v", err)
		}
		if len(schemas) != 3 {
			t.Fatalf("schemas = %d, want 3", len(schemas))
		}

		got := []string{schemas[0].Name, schemas[1].Name, schemas[2].Name}
		want := []string{"a-first", "m-middle", "z-last"}
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("schemas order = %#v, want %#v", got, want)
			}
		}
	}
}

type namedEchoTool struct {
	name string
}

func (t *namedEchoTool) Name() string        { return t.name }
func (t *namedEchoTool) Description() string { return t.name }
func (t *namedEchoTool) Kind() tools.Kind    { return tools.KindNative }
func (t *namedEchoTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        t.name,
		Description: t.name,
		Parameters:  llm.SchemaProperty{Type: "object"},
	}
}
func (t *namedEchoTool) Execute(context.Context, map[string]any) (string, error) {
	return t.name, nil
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
