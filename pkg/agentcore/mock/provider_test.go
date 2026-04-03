package mock_test

import (
	stdctx "context"
	"errors"
	"testing"

	"github.com/inhere/axon/pkg/agentcore"
	"github.com/inhere/axon/pkg/agentcore/mock"
)

func TestDefaultRegistrySchemas(t *testing.T) {
	reg := agentcore.NewToolRegistry()
	reg.Register(mock.NewTool(
		"echo",
		mock.WithDescription("echoes input text"),
		mock.WithSchema(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
			},
			"required": []any{"text"},
		}),
	))

	schemas, err := reg.ListTools(stdctx.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(schemas) != 1 {
		t.Fatalf("expected 1 schema, got %d", len(schemas))
	}
	if schemas[0].Name != "echo" {
		t.Fatalf("expected schema name echo, got %q", schemas[0].Name)
	}
	if schemas[0].Description != "echoes input text" {
		t.Fatalf("expected schema description to be preserved, got %q", schemas[0].Description)
	}
	if schemas[0].InputSchema["type"] != "object" {
		t.Fatalf("expected schema type object, got %#v", schemas[0].InputSchema["type"])
	}

	properties, ok := schemas[0].InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties map, got %#v", schemas[0].InputSchema["properties"])
	}
	textField, ok := properties["text"].(map[string]any)
	if !ok {
		t.Fatalf("expected text property schema, got %#v", properties["text"])
	}
	if textField["type"] != "string" {
		t.Fatalf("expected text property type string, got %#v", textField["type"])
	}
}

func TestMockProvider_ReturnsScriptedStepsInOrder(t *testing.T) {
	provider := mock.NewProvider(
		mock.ToolCall("call-1", "echo", map[string]any{"text": "x"}),
		mock.Final("hello"),
	)

	first, err := provider.Generate(stdctx.Background(), &agentcore.GenerateRequest{})
	if err != nil {
		t.Fatalf("expected no error from first step, got %v", err)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].Name != "echo" {
		t.Fatalf("expected scripted tool call, got %#v", first)
	}

	second, err := provider.Generate(stdctx.Background(), &agentcore.GenerateRequest{})
	if err != nil {
		t.Fatalf("expected no error from second step, got %v", err)
	}
	if second.Content != "hello" || second.FinishReason != agentcore.FinishFinal {
		t.Fatalf("expected scripted final response, got %#v", second)
	}
}

func TestMockProvider_ReturnsScriptedErrorStep(t *testing.T) {
	wantErr := errors.New("provider step failed")
	provider := mock.NewProvider(mock.Error(wantErr))

	result, err := provider.Generate(stdctx.Background(), &agentcore.GenerateRequest{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
	if result != nil {
		t.Fatalf("expected no result for error step, got %#v", result)
	}
}

func TestMockProvider_ErrorsWhenExhausted(t *testing.T) {
	provider := mock.NewProvider(mock.Final("hello"))

	_, err := provider.Generate(stdctx.Background(), &agentcore.GenerateRequest{})
	if err != nil {
		t.Fatalf("expected first scripted step to succeed, got %v", err)
	}

	result, err := provider.Generate(stdctx.Background(), &agentcore.GenerateRequest{})
	if err == nil || err.Error() != "mock provider exhausted" {
		t.Fatalf("expected exhausted error, got result=%#v err=%v", result, err)
	}
	if result != nil {
		t.Fatalf("expected no result when provider is exhausted, got %#v", result)
	}
}

func TestMockTool_EchoesTextArgument(t *testing.T) {
	tool := mock.NewTool("echo")

	result, err := tool.Execute(stdctx.Background(), agentcore.ToolCall{
		CallID:    "call-1",
		Name:      "echo",
		Arguments: map[string]any{"text": "hello"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Content != "hello" {
		t.Fatalf("expected echoed text, got %q", result.Content)
	}
}
