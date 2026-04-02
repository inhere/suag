package mock_test

import (
	stdctx "context"
	"testing"

	"github.com/inhere/axon/pkg/agentcore"
	"github.com/inhere/axon/pkg/agentcore/mock"
)

func TestDefaultRegistrySchemas(t *testing.T) {
	reg := agentcore.NewToolRegistry()
	reg.Register(mock.NewTool("echo"))

	schemas := reg.Schemas()
	if len(schemas) != 1 {
		t.Fatalf("expected 1 schema, got %d", len(schemas))
	}
	if schemas[0].Name != "echo" {
		t.Fatalf("expected schema name echo, got %q", schemas[0].Name)
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
