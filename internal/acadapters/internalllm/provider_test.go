package internalllm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/inhere/axon/internal/acadapters/internalllm"
	"github.com/inhere/axon/internal/platform/llm"
	"github.com/inhere/axon/pkg/agentcore/api"
)

type stubProvider struct {
	name      string
	resp      *llm.CompletionResponse
	err       error
	lastReq   *llm.CompletionRequest
	callCount int
}

func (s *stubProvider) Name() string { return s.name }

func (s *stubProvider) Generate(ctx context.Context, req *api.GenerateRequest) (*api.GenerateResult, error) {
	resp, err := s.Complete(ctx, &llm.CompletionRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		System:      req.System,
		Messages:    toLegacyMessages(req.Messages),
		Tools:       toLegacyTools(req.Tools),
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, nil
	}
	result := &api.GenerateResult{
		Content: resp.Content,
		Usage: api.Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		},
	}
	switch resp.FinishReason {
	case "tool_calls":
		result.FinishReason = api.FinishToolCalls
	default:
		result.FinishReason = api.FinishFinal
	}
	if len(resp.ToolCalls) > 0 {
		result.ToolCalls = make([]api.ToolCall, len(resp.ToolCalls))
		for i, call := range resp.ToolCalls {
			result.ToolCalls[i] = api.ToolCall{
				CallID:    call.ID,
				Name:      call.Name,
				Arguments: cloneMap(call.Arguments),
			}
		}
	}
	return result, nil
}

func (s *stubProvider) Complete(_ context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	s.callCount++
	s.lastReq = cloneCompletionRequest(req)
	if s.err != nil {
		return nil, s.err
	}
	return cloneCompletionResponse(s.resp), nil
}

func (s *stubProvider) StreamText(context.Context, *llm.CompletionRequest) (<-chan string, error) {
	return nil, errors.New("not implemented")
}

func (s *stubProvider) Embed(context.Context, string) ([]float32, error) {
	return nil, errors.New("not implemented")
}

func TestInternalLLMAdapter_MapsFinishReasons(t *testing.T) {
	tests := []struct {
		name         string
		finishReason string
		wantReason   api.FinishReason
		wantErr      bool
	}{
		{name: "stop maps to final", finishReason: "stop", wantReason: api.FinishFinal},
		{name: "empty maps to final", finishReason: "", wantReason: api.FinishFinal},
		{name: "tool calls map through", finishReason: "tool_calls", wantReason: api.FinishToolCalls},
		{name: "length returns provider error", finishReason: "length", wantErr: true},
		{name: "unknown returns provider error", finishReason: "content_filter", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := &llm.CompletionResponse{
				Content:      "hello",
				FinishReason: tc.finishReason,
			}
			if tc.finishReason == "tool_calls" {
				resp.Content = ""
				resp.ToolCalls = []llm.ToolCall{{
					ID:   "call-1",
					Name: "lookup",
				}}
			}

			internal := &stubProvider{
				name: "stub",
				resp: resp,
			}

			provider := internalllm.NewProvider(internal)
			result, err := provider.Generate(context.Background(), &api.GenerateRequest{
				Messages: []api.Message{{Role: "user", Content: "hi"}},
			})

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for finish reason %q", tc.finishReason)
				}
				var providerErr *api.ProviderError
				if !errors.As(err, &providerErr) {
					t.Fatalf("expected provider error wrapper, got %T %v", err, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if result.FinishReason != tc.wantReason {
				t.Fatalf("finish reason = %q, want %q", result.FinishReason, tc.wantReason)
			}
		})
	}
}

func TestInternalLLMAdapter_MapsMessagesAndToolCalls(t *testing.T) {
	internal := &stubProvider{
		name: "internal-mock",
		resp: &llm.CompletionResponse{
			ToolCalls: []llm.ToolCall{{
				ID:   "call-1",
				Name: "lookup",
				Arguments: map[string]any{
					"topic": "weather",
				},
			}},
			Usage: llm.TokenUsage{
				PromptTokens:     11,
				CompletionTokens: 7,
				TotalTokens:      18,
			},
			FinishReason: "tool_calls",
		},
	}

	provider := internalllm.NewProvider(internal)
	result, err := provider.Generate(context.Background(), &api.GenerateRequest{
		Model:       "internal-test",
		System:      "system prompt",
		MaxTokens:   256,
		Temperature: 0.3,
		Metadata:    map[string]any{"trace_id": "trace-1"},
		Messages: []api.Message{
			{Role: "system", Content: "system message"},
			{Role: "user", Content: "find weather"},
			{
				Role:     "assistant",
				ToolCall: &api.ToolCall{CallID: "call-0", Name: "lookup", Arguments: map[string]any{"topic": "history"}},
			},
			{
				Role:       "tool",
				Content:    "sunny",
				ToolCall:   &api.ToolCall{CallID: "call-0", Name: "lookup"},
				ToolResult: &api.ToolResult{CallID: "call-0", Name: "lookup", Content: "sunny"},
			},
		},
		Tools: []api.ToolSchema{{
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
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if internal.callCount != 1 {
		t.Fatalf("expected one internal completion call, got %d", internal.callCount)
	}
	if internal.lastReq == nil {
		t.Fatal("expected internal provider request to be captured")
	}
	if internal.lastReq.Model != "internal-test" {
		t.Fatalf("model = %q, want internal-test", internal.lastReq.Model)
	}
	if internal.lastReq.System != "system prompt" {
		t.Fatalf("system = %q, want system prompt", internal.lastReq.System)
	}
	if len(internal.lastReq.Messages) != 4 {
		t.Fatalf("message count = %d, want 4", len(internal.lastReq.Messages))
	}
	if internal.lastReq.Messages[2].ToolCall == nil || internal.lastReq.Messages[2].ToolCall.ID != "call-0" {
		t.Fatalf("assistant tool call not mapped, got %#v", internal.lastReq.Messages[2])
	}
	if internal.lastReq.Messages[3].ToolResult == nil || internal.lastReq.Messages[3].ToolResult.ToolCallID != "call-0" {
		t.Fatalf("tool result not mapped, got %#v", internal.lastReq.Messages[3])
	}
	if len(internal.lastReq.Tools) != 1 {
		t.Fatalf("tool schemas = %d, want 1", len(internal.lastReq.Tools))
	}
	if internal.lastReq.Tools[0].Parameters.Type != "object" {
		t.Fatalf("tool schema type = %q, want object", internal.lastReq.Tools[0].Parameters.Type)
	}
	if internal.lastReq.Tools[0].Parameters.Properties["topic"] == nil {
		t.Fatalf("expected topic schema property, got %#v", internal.lastReq.Tools[0].Parameters)
	}
	if len(internal.lastReq.Tools[0].Parameters.Properties["topic"].Enum) != 2 ||
		internal.lastReq.Tools[0].Parameters.Properties["topic"].Enum[0] != "weather" ||
		internal.lastReq.Tools[0].Parameters.Properties["topic"].Enum[1] != "history" {
		t.Fatalf("expected enum constraint to be preserved, got %#v", internal.lastReq.Tools[0].Parameters.Properties["topic"])
	}
	if len(internal.lastReq.Tools[0].Parameters.Required) != 1 || internal.lastReq.Tools[0].Parameters.Required[0] != "topic" {
		t.Fatalf("required fields = %#v, want [topic]", internal.lastReq.Tools[0].Parameters.Required)
	}

	if result.FinishReason != api.FinishToolCalls {
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

func cloneCompletionRequest(req *llm.CompletionRequest) *llm.CompletionRequest {
	if req == nil {
		return nil
	}
	cloned := &llm.CompletionRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		System:      req.System,
	}
	if len(req.Messages) > 0 {
		cloned.Messages = make([]llm.Message, len(req.Messages))
		for i, msg := range req.Messages {
			cloned.Messages[i] = llm.Message{
				Role:       msg.Role,
				Content:    msg.Content,
				ToolCall:   cloneLLMToolCall(msg.ToolCall),
				ToolResult: cloneLLMToolResult(msg.ToolResult),
			}
		}
	}
	if len(req.Tools) > 0 {
		cloned.Tools = make([]llm.ToolSchema, len(req.Tools))
		copy(cloned.Tools, req.Tools)
	}
	return cloned
}

func cloneCompletionResponse(resp *llm.CompletionResponse) *llm.CompletionResponse {
	if resp == nil {
		return nil
	}
	cloned := &llm.CompletionResponse{
		Content:      resp.Content,
		Usage:        resp.Usage,
		FinishReason: resp.FinishReason,
	}
	if len(resp.ToolCalls) > 0 {
		cloned.ToolCalls = make([]llm.ToolCall, len(resp.ToolCalls))
		for i, call := range resp.ToolCalls {
			cloned.ToolCalls[i] = llm.ToolCall{
				ID:        call.ID,
				Name:      call.Name,
				Arguments: cloneMap(call.Arguments),
			}
		}
	}
	return cloned
}

func cloneLLMToolCall(call *llm.ToolCall) *llm.ToolCall {
	if call == nil {
		return nil
	}
	return &llm.ToolCall{
		ID:        call.ID,
		Name:      call.Name,
		Arguments: cloneMap(call.Arguments),
	}
}

func cloneLLMToolResult(result *llm.ToolResult) *llm.ToolResult {
	if result == nil {
		return nil
	}
	return &llm.ToolResult{
		ToolCallID: result.ToolCallID,
		Content:    result.Content,
	}
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		switch typed := v.(type) {
		case map[string]any:
			dst[k] = cloneMap(typed)
		case []any:
			items := make([]any, len(typed))
			for i, item := range typed {
				if nested, ok := item.(map[string]any); ok {
					items[i] = cloneMap(nested)
					continue
				}
				items[i] = item
			}
			dst[k] = items
		default:
			dst[k] = v
		}
	}
	return dst
}

func toLegacyMessages(messages []api.Message) []llm.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]llm.Message, len(messages))
	for i, msg := range messages {
		out[i] = llm.Message{
			Role:    msg.Role,
			Content: msg.Content,
		}
		if msg.ToolCall != nil {
			out[i].ToolCall = &llm.ToolCall{
				ID:        msg.ToolCall.CallID,
				Name:      msg.ToolCall.Name,
				Arguments: cloneMap(msg.ToolCall.Arguments),
			}
		}
		if msg.ToolResult != nil {
			out[i].ToolResult = &llm.ToolResult{
				ToolCallID: msg.ToolResult.CallID,
				Content:    msg.ToolResult.Content,
			}
		}
	}
	return out
}

func toLegacyTools(tools []api.ToolSchema) []llm.ToolSchema {
	if len(tools) == 0 {
		return nil
	}
	out := make([]llm.ToolSchema, len(tools))
	for i, tool := range tools {
		out[i] = llm.ToolSchema{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  toSchemaProperty(tool.InputSchema),
		}
	}
	return out
}

func toSchemaProperty(schema map[string]any) llm.SchemaProperty {
	prop := llm.SchemaProperty{}
	if len(schema) == 0 {
		return prop
	}
	if value, ok := schema["type"].(string); ok {
		prop.Type = value
	}
	if value, ok := schema["description"].(string); ok {
		prop.Description = value
	}
	switch enum := schema["enum"].(type) {
	case []string:
		prop.Enum = append([]string(nil), enum...)
	case []any:
		prop.Enum = make([]string, 0, len(enum))
		for _, item := range enum {
			if text, ok := item.(string); ok {
				prop.Enum = append(prop.Enum, text)
			}
		}
	}
	switch required := schema["required"].(type) {
	case []string:
		prop.Required = append([]string(nil), required...)
	case []any:
		prop.Required = make([]string, 0, len(required))
		for _, item := range required {
			if text, ok := item.(string); ok {
				prop.Required = append(prop.Required, text)
			}
		}
	}
	if properties, ok := schema["properties"].(map[string]any); ok {
		prop.Properties = make(map[string]*llm.SchemaProperty, len(properties))
		for name, raw := range properties {
			if nested, ok := raw.(map[string]any); ok {
				child := toSchemaProperty(nested)
				prop.Properties[name] = &child
			}
		}
	}
	return prop
}
