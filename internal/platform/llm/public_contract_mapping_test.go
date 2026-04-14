package llm

import "testing"

func TestToGenerateResult_MapsToolCallsAndUsage(t *testing.T) {
	result := toGenerateResult(MockResponse{
		ToolCalls: []ToolCall{{
			ID:   "call-1",
			Name: "lookup",
			Arguments: map[string]any{
				"topic": "weather",
			},
		}},
		FinishReason: "tool_calls",
	})

	if result == nil {
		t.Fatal("expected generate result to be built")
	}
	if result.FinishReason != FinishToolCalls {
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
}
