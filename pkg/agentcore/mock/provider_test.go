package mock

import (
	stdctx "context"
	"errors"
	"reflect"
	"testing"

	"github.com/inhere/axon/pkg/agentcore"
)

func TestDefaultRegistrySchemas(t *testing.T) {
	reg := agentcore.NewToolRegistry()
	reg.Register(NewTool(
		"echo",
		WithDescription("echoes input text"),
		WithSchema(map[string]any{
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
	provider := NewProvider(
		ToolCall("call-1", "echo", map[string]any{"text": "x"}),
		Final("hello"),
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
	provider := NewProvider(Error(wantErr))

	result, err := provider.Generate(stdctx.Background(), &agentcore.GenerateRequest{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
	if result != nil {
		t.Fatalf("expected no result for error step, got %#v", result)
	}
}

func TestMockProvider_RejectsInvalidScriptedResult(t *testing.T) {
	provider := NewProvider(Step{
		result: &agentcore.GenerateResult{
			Content:      "hello",
			FinishReason: agentcore.FinishToolCalls,
		},
	})

	result, err := provider.Generate(stdctx.Background(), &agentcore.GenerateRequest{})
	if !errors.Is(err, agentcore.ErrInvalidFinishReason) {
		t.Fatalf("expected invalid finish reason error, got result=%#v err=%v", result, err)
	}
	if result != nil {
		t.Fatalf("expected no result for invalid scripted output, got %#v", result)
	}
}

func TestMockProvider_ErrorsWhenExhausted(t *testing.T) {
	provider := NewProvider(Final("hello"))

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

func TestMockProvider_ClonesNestedInputsAndResults(t *testing.T) {
	arguments := map[string]any{
		"payload": map[string]any{
			"message": "original",
			"items":   []any{"first", map[string]any{"nested": "value"}},
		},
	}
	metadata := map[string]any{
		"trace": map[string]any{
			"steps": []any{"a", map[string]any{"state": "ready"}},
		},
	}

	step := Step{
		result: &agentcore.GenerateResult{
			ToolCalls: []agentcore.ToolCall{{
				CallID:    "call-1",
				Name:      "echo",
				Arguments: arguments,
			}},
			FinishReason: agentcore.FinishToolCalls,
			Metadata:     metadata,
			Artifacts: []agentcore.Artifact{{
				Kind: "file",
				URI:  "artifact://1",
				Metadata: map[string]any{
					"payload": map[string]any{
						"status": "ready",
					},
				},
			}},
		},
	}

	provider := NewProvider(step, step)

	arguments["payload"].(map[string]any)["message"] = "mutated outside"
	arguments["payload"].(map[string]any)["items"].([]any)[1].(map[string]any)["nested"] = "changed outside"
	metadata["trace"].(map[string]any)["steps"].([]any)[1].(map[string]any)["state"] = "changed outside"

	first, err := provider.Generate(stdctx.Background(), &agentcore.GenerateRequest{})
	if err != nil {
		t.Fatalf("expected first scripted step to succeed, got %v", err)
	}

	if got := first.ToolCalls[0].Arguments["payload"].(map[string]any)["message"]; got != "original" {
		t.Fatalf("expected provider to clone nested input maps, got %#v", got)
	}
	if got := first.ToolCalls[0].Arguments["payload"].(map[string]any)["items"].([]any)[1].(map[string]any)["nested"]; got != "value" {
		t.Fatalf("expected provider to clone nested input slices, got %#v", got)
	}
	if got := first.Metadata["trace"].(map[string]any)["steps"].([]any)[1].(map[string]any)["state"]; got != "ready" {
		t.Fatalf("expected provider metadata to be isolated from caller mutations, got %#v", got)
	}
	if got := first.Artifacts[0].Metadata["payload"].(map[string]any)["status"]; got != "ready" {
		t.Fatalf("expected provider artifacts to clone nested metadata, got %#v", got)
	}

	first.ToolCalls[0].Arguments["payload"].(map[string]any)["message"] = "mutated return"
	first.ToolCalls[0].Arguments["payload"].(map[string]any)["items"].([]any)[1].(map[string]any)["nested"] = "mutated return"
	first.Metadata["trace"].(map[string]any)["steps"].([]any)[1].(map[string]any)["state"] = "mutated return"
	first.Artifacts[0].Metadata["payload"].(map[string]any)["status"] = "mutated return"

	second, err := provider.Generate(stdctx.Background(), &agentcore.GenerateRequest{})
	if err != nil {
		t.Fatalf("expected second scripted step to succeed, got %v", err)
	}

	if got := second.ToolCalls[0].Arguments["payload"].(map[string]any)["message"]; got != "original" {
		t.Fatalf("expected returned tool call arguments to be cloned on each read, got %#v", got)
	}
	if got := second.ToolCalls[0].Arguments["payload"].(map[string]any)["items"].([]any)[1].(map[string]any)["nested"]; got != "value" {
		t.Fatalf("expected returned nested slice values to stay isolated, got %#v", got)
	}
	if got := second.Metadata["trace"].(map[string]any)["steps"].([]any)[1].(map[string]any)["state"]; got != "ready" {
		t.Fatalf("expected returned metadata to be cloned on each read, got %#v", got)
	}
	if got := second.Artifacts[0].Metadata["payload"].(map[string]any)["status"]; got != "ready" {
		t.Fatalf("expected returned artifact metadata to be cloned on each read, got %#v", got)
	}
}

func TestMockProvider_GenerateHandlesNilReceiver(t *testing.T) {
	var provider *Provider

	result, err := provider.Generate(stdctx.Background(), &agentcore.GenerateRequest{})
	if err == nil || err.Error() != "mock provider exhausted" {
		t.Fatalf("expected exhausted error for nil provider, got result=%#v err=%v", result, err)
	}
	if result != nil {
		t.Fatalf("expected nil result for nil provider, got %#v", result)
	}
}

func TestMockTool_EchoesTextArgument(t *testing.T) {
	tool := NewTool("echo")

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

func TestMockTool_SchemaClonesNestedInputAndReturnValues(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"payload": map[string]any{
				"type": "object",
				"items": []any{
					"first",
					map[string]any{"type": "string"},
				},
			},
		},
		"required": []any{"payload"},
	}

	tool := NewTool("echo", WithSchema(schema))

	schema["properties"].(map[string]any)["payload"].(map[string]any)["type"] = "array"
	schema["properties"].(map[string]any)["payload"].(map[string]any)["items"].([]any)[1].(map[string]any)["type"] = "number"
	schema["required"].([]any)[0] = "changed"

	first := tool.Schema()
	if got := first.InputSchema["properties"].(map[string]any)["payload"].(map[string]any)["type"]; got != "object" {
		t.Fatalf("expected tool schema to clone nested input maps, got %#v", got)
	}
	if got := first.InputSchema["properties"].(map[string]any)["payload"].(map[string]any)["items"].([]any)[1].(map[string]any)["type"]; got != "string" {
		t.Fatalf("expected tool schema to clone nested input slices, got %#v", got)
	}
	if !reflect.DeepEqual(first.InputSchema["required"], []any{"payload"}) {
		t.Fatalf("expected required list to remain unchanged, got %#v", first.InputSchema["required"])
	}

	first.InputSchema["properties"].(map[string]any)["payload"].(map[string]any)["type"] = "mutated return"
	first.InputSchema["properties"].(map[string]any)["payload"].(map[string]any)["items"].([]any)[1].(map[string]any)["type"] = "mutated return"
	first.InputSchema["required"].([]any)[0] = "mutated return"

	second := tool.Schema()
	if got := second.InputSchema["properties"].(map[string]any)["payload"].(map[string]any)["type"]; got != "object" {
		t.Fatalf("expected returned tool schema maps to be cloned on each read, got %#v", got)
	}
	if got := second.InputSchema["properties"].(map[string]any)["payload"].(map[string]any)["items"].([]any)[1].(map[string]any)["type"]; got != "string" {
		t.Fatalf("expected returned tool schema slices to be cloned on each read, got %#v", got)
	}
	if !reflect.DeepEqual(second.InputSchema["required"], []any{"payload"}) {
		t.Fatalf("expected returned required list to stay isolated, got %#v", second.InputSchema["required"])
	}
}
