package llm

import (
	"testing"

	agentcoreapi "github.com/inhere/axon/pkg/agentcore/api"
)

func TestFromGenerateRequest_MapsMessagesAndToolSchemas(t *testing.T) {
	req := fromGenerateRequest(&agentcoreapi.GenerateRequest{
		Model:       "internal-test",
		System:      "system prompt",
		MaxTokens:   256,
		Temperature: 0.3,
		Messages: []agentcoreapi.Message{
			{Role: "system", Content: "system message"},
			{Role: "user", Content: "find weather"},
			{
				Role:     "assistant",
				ToolCall: &agentcoreapi.ToolCall{CallID: "call-0", Name: "lookup", Arguments: map[string]any{"topic": "history"}},
			},
			{
				Role:       "tool",
				Content:    "sunny",
				ToolCall:   &agentcoreapi.ToolCall{CallID: "call-0", Name: "lookup"},
				ToolResult: &agentcoreapi.ToolResult{CallID: "call-0", Name: "lookup", Content: "sunny"},
			},
		},
		Tools: []agentcoreapi.ToolSchema{{
			Name:        "lookup",
			Description: "Look up data",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"topic": map[string]any{
						"type":        "string",
						"description": "topic to search",
						"enum":        []any{"weather", "history"},
					},
				},
				"required": []any{"topic"},
			},
		}},
	})

	if req == nil {
		t.Fatal("expected completion request to be built")
	}
	if req.Model != "internal-test" {
		t.Fatalf("model = %q, want internal-test", req.Model)
	}
	if req.System != "system prompt" {
		t.Fatalf("system = %q, want system prompt", req.System)
	}
	if len(req.Messages) != 4 {
		t.Fatalf("message count = %d, want 4", len(req.Messages))
	}
	if req.Messages[2].ToolCall == nil || req.Messages[2].ToolCall.ID != "call-0" {
		t.Fatalf("assistant tool call not mapped, got %#v", req.Messages[2])
	}
	if req.Messages[3].ToolResult == nil || req.Messages[3].ToolResult.ToolCallID != "call-0" {
		t.Fatalf("tool result not mapped, got %#v", req.Messages[3])
	}
	if len(req.Tools) != 1 {
		t.Fatalf("tool schemas = %d, want 1", len(req.Tools))
	}
	if req.Tools[0].Parameters.Type != "object" {
		t.Fatalf("tool schema type = %q, want object", req.Tools[0].Parameters.Type)
	}
	if req.Tools[0].Parameters.Properties["topic"] == nil {
		t.Fatalf("expected topic schema property, got %#v", req.Tools[0].Parameters)
	}
	if len(req.Tools[0].Parameters.Properties["topic"].Enum) != 2 ||
		req.Tools[0].Parameters.Properties["topic"].Enum[0] != "weather" ||
		req.Tools[0].Parameters.Properties["topic"].Enum[1] != "history" {
		t.Fatalf("expected enum constraint to be preserved, got %#v", req.Tools[0].Parameters.Properties["topic"])
	}
	if len(req.Tools[0].Parameters.Required) != 1 || req.Tools[0].Parameters.Required[0] != "topic" {
		t.Fatalf("required fields = %#v, want [topic]", req.Tools[0].Parameters.Required)
	}
}

func TestToGenerateResult_MapsToolCallsAndUsage(t *testing.T) {
	result := toGenerateResult(&CompletionResponse{
		ToolCalls: []ToolCall{{
			ID:   "call-1",
			Name: "lookup",
			Arguments: map[string]any{
				"topic": "weather",
			},
		}},
		Usage: TokenUsage{
			PromptTokens:     11,
			CompletionTokens: 7,
			TotalTokens:      18,
		},
		FinishReason: "tool_calls",
	})

	if result == nil {
		t.Fatal("expected generate result to be built")
	}
	if result.FinishReason != agentcoreapi.FinishToolCalls {
		t.Fatalf("finish reason = %q, want tool_calls", result.FinishReason)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(result.ToolCalls))
	}
	if result.ToolCalls[0].CallID != "call-1" || result.ToolCalls[0].Name != "lookup" {
		t.Fatalf("unexpected tool call mapping: %#v", result.ToolCalls[0])
	}
	if result.ToolCalls[0].Arguments["topic"] != "weather" {
		t.Fatalf("unexpected tool call arguments: %#v", result.ToolCalls[0].Arguments)
	}
	if result.Usage.InputTokens != 11 || result.Usage.OutputTokens != 7 || result.Usage.TotalTokens != 18 {
		t.Fatalf("unexpected usage mapping: %#v", result.Usage)
	}
}
