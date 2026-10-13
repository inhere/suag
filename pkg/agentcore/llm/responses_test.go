package llm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"

	agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"
)

func TestResolveCompat(t *testing.T) {
	c := ResolveCompat("https://api.commandcode.ai/provider/v1", "deepseek/deepseek-v4.1-flash")
	assert.Eq(t, []string{"commandcode", "deepseek"}, c.Names)
	assert.True(t, c.ReasoningSummary)
	assert.True(t, c.EncryptedReasoning)
	assert.True(t, c.ReasoningEveryTurn)
	assert.True(t, c.NoToolChoice)
	assert.Eq(t, 64000, c.MaxOutputTokens)
	assert.Eq(t, "prompt_cache_hit_tokens", c.CacheHitField)

	assert.Eq(t, []string{"openai"}, ResolveCompat("https://api.openai.com/v1", "gpt-5.5").Names)
	assert.Nil(t, ResolveCompat("https://gw.example/v1", "glm-5").Names)
	assert.Eq(t, []string{"deepseek"}, ResolveCompat("https://gw.example/v1", "my-model", "DeepSeek", "nope").Names)
}

// responsesHistory: a user turn, a reply with two parallel calls (its
// reasoning stored as returned), their results, then a text reply without
// stored reasoning.
func responsesHistory() []agentcoreapi.Message {
	batch := map[string]any{agentcoreapi.ToolBatchMetaKey: "c1"}
	first := map[string]any{agentcoreapi.ToolBatchMetaKey: "c1", ResponsesReasoningMetaKey: []any{
		map[string]any{"type": "reasoning", "id": "rs_1", "status": "completed", "encrypted_content": "enc",
			"summary": []any{map[string]any{"type": "summary_text", "text": "read both"}}},
	}}
	c1 := &agentcoreapi.ToolCall{CallID: "c1", Name: "file_read", Arguments: map[string]any{"path": "a.go"}}
	c2 := &agentcoreapi.ToolCall{CallID: "c2", Name: "file_read", Arguments: map[string]any{"path": "b.go"}}
	return []agentcoreapi.Message{
		{Role: "user", Content: "fix it"},
		{Role: "assistant", Content: "reading", Reasoning: "read both", ToolCall: c1, Metadata: first},
		{Role: "tool", ToolCall: c1, ToolResult: &agentcoreapi.ToolResult{CallID: "c1", Content: "A"}},
		{Role: "assistant", ToolCall: c2, Metadata: batch},
		{Role: "tool", ToolCall: c2, ToolResult: &agentcoreapi.ToolResult{CallID: "c2", Content: "B"}},
		{Role: "assistant", Content: "done"},
	}
}

func TestResponsesRequestBody(t *testing.T) {
	p := NewResponsesProvider(ResponsesConfig{OpenAIConfig: OpenAIConfig{
		BaseURL: "https://api.commandcode.ai/provider/v1", Model: "deepseek/deepseek-v4.1-flash", ReasoningEffort: "high"}})
	ctx := WithSessionID(context.Background(), "sess-1")
	body := p.buildResponsesBody(ctx, &agentcoreapi.GenerateRequest{System: "be brief", Messages: responsesHistory(),
		Tools: []agentcoreapi.ToolSchema{{Name: "file_read", Description: "Read", InputSchema: map[string]any{"type": "object"}}}}, true)
	raw, _ := json.Marshal(body)
	var got map[string]any
	assert.NoErr(t, json.Unmarshal(raw, &got))

	assert.Eq(t, false, got["store"])
	assert.Eq(t, true, got["stream"])
	assert.Eq(t, "be brief", got["instructions"])
	assert.Eq(t, map[string]any{"effort": "high", "summary": "auto"}, got["reasoning"])
	assert.Eq(t, []any{"reasoning.encrypted_content"}, got["include"])
	assert.Eq(t, float64(64000), got["max_output_tokens"])
	assert.Eq(t, "sess-1", got["prompt_cache_key"])
	assert.Nil(t, got["tool_choice"])
	assert.Eq(t, []any{map[string]any{"type": "function", "name": "file_read", "description": "Read",
		"parameters": map[string]any{"type": "object"}}}, got["tools"])

	input := got["input"].([]any)
	kinds := make([]string, len(input))
	for i, it := range input {
		m := it.(map[string]any)
		kinds[i], _ = m["type"].(string)
		if kinds[i] == "" {
			kinds[i] = m["role"].(string)
		}
	}
	// parallel calls regrouped; each assistant turn opens with reasoning
	assert.Eq(t, []string{"user", "reasoning", "message", "function_call", "function_call",
		"function_call_output", "function_call_output", "reasoning", "message"}, kinds)
	assert.Eq(t, map[string]any{"type": "reasoning", "encrypted_content": "enc",
		"summary": []any{map[string]any{"type": "summary_text", "text": "read both"}}}, input[1]) // no id, no status
	assert.Eq(t, map[string]any{"type": "function_call", "call_id": "c2", "name": "file_read", "arguments": `{"path":"b.go"}`}, input[4])
	assert.Eq(t, map[string]any{"type": "function_call_output", "call_id": "c1", "output": "A"}, input[5])
	assert.Eq(t, reasoningPlaceholder, input[7].(map[string]any)["content"].([]any)[0].(map[string]any)["text"])
}

func TestResponsesReasoningReplay(t *testing.T) {
	deepseek := NewResponsesProvider(ResponsesConfig{OpenAIConfig: OpenAIConfig{BaseURL: "https://gw.example/v1", Model: "deepseek-v4"}})
	plain := NewResponsesProvider(ResponsesConfig{OpenAIConfig: OpenAIConfig{BaseURL: "https://gw.example/v1", Model: "glm-5"}})
	call := &agentcoreapi.ToolCall{CallID: "c1", Name: "x"}
	summaryOnly := agentcoreapi.Message{Role: "assistant", Reasoning: "plan", ToolCall: call, Metadata: map[string]any{
		ResponsesReasoningMetaKey: []any{map[string]any{"type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": "plan"}}}}}}

	t.Run("a stored item without text or encryption gets its summary as text", func(t *testing.T) {
		items := deepseek.turnReasoning(summaryOnly, true)
		assert.Eq(t, []map[string]any{{"type": "reasoning_text", "text": "plan"}}, items[0]["content"])
		assert.Nil(t, plain.turnReasoning(summaryOnly, true)[0]["content"]) // only DeepSeek needs it
	})
	t.Run("a folded turn sends a placeholder, not its reasoning", func(t *testing.T) {
		folded := summaryOnly
		folded.Reasoning = ""
		folded.Metadata = map[string]any{ResponsesReasoningMetaKey: summaryOnly.Metadata[ResponsesReasoningMetaKey], ReasoningFoldedMetaKey: true}
		items := deepseek.turnReasoning(folded, true)
		assert.Eq(t, reasoningPlaceholder, items[0]["content"].([]map[string]any)[0]["text"])
		assert.Nil(t, plain.turnReasoning(folded, true))
	})
	t.Run("no thinking, nothing filled in", func(t *testing.T) {
		assert.Nil(t, deepseek.turnReasoning(agentcoreapi.Message{Role: "assistant", Content: "hi"}, false))
	})
	t.Run("off is none, without summary or include", func(t *testing.T) {
		p := NewResponsesProvider(ResponsesConfig{OpenAIConfig: OpenAIConfig{BaseURL: "https://api.openai.com/v1", Model: "gpt-5.5", ReasoningEffort: "off"}})
		body := p.buildResponsesBody(context.Background(), &agentcoreapi.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "x"}}}, false)
		assert.Eq(t, map[string]any{"effort": "none"}, body["reasoning"])
		assert.Nil(t, body["include"])
		assert.Nil(t, body["max_output_tokens"])
	})
}

const responsesResult = `{"status":"completed","output":[
 {"type":"reasoning","id":"rs_9","status":"completed","encrypted_content":"enc9","summary":[{"type":"summary_text","text":"look at a.go"}]},
 {"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"Reading.","annotations":[]}]},
 {"type":"function_call","id":"fc_1","call_id":"call_1","name":"file_read","arguments":"{\"path\":\"a.go\"}"}],
 "usage":{"input_tokens":1200,"input_tokens_details":{"cached_tokens":0},"prompt_cache_hit_tokens":1000,"output_tokens":80}}`

func TestResponsesParse(t *testing.T) {
	p := NewResponsesProvider(ResponsesConfig{OpenAIConfig: OpenAIConfig{BaseURL: "https://api.commandcode.ai/provider/v1", Model: "deepseek/deepseek-v4.1-flash"}})
	res, err := p.parseResponsesBody([]byte(responsesResult))
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, "Reading.", res.Content)
	assert.Eq(t, "look at a.go", res.Reasoning)
	assert.Eq(t, agentcoreapi.FinishToolCalls, res.FinishReason)
	assert.Eq(t, "call_1", res.ToolCalls[0].CallID)
	assert.Eq(t, map[string]any{"path": "a.go"}, res.ToolCalls[0].Arguments)
	assert.Eq(t, agentcoreapi.Usage{InputTokens: 1200, OutputTokens: 80, TotalTokens: 1280, CacheReadTokens: 1000}, res.Usage)
	stored := res.Metadata[ResponsesReasoningMetaKey].([]any)[0].(map[string]any)
	assert.Nil(t, stored["id"])
	assert.Nil(t, stored["status"])

	t.Run("cut off at max_output_tokens: calls are not run", func(t *testing.T) {
		cut := strings.Replace(responsesResult, `"status":"completed"`, `"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}`, 1)
		res, err := p.parseResponsesBody([]byte(cut))
		assert.Require(t, assert.NoErr(t, err))
		assert.NotEmpty(t, res.ToolCalls[0].ArgumentsError)
	})
	t.Run("errors", func(t *testing.T) {
		_, err := p.parseResponsesBody([]byte(`{"error":{"message":"bad effort"}}`))
		assert.ErrMsg(t, err, "responses api error: bad effort")
		_, err = p.parseResponsesBody([]byte(`{"status":"incomplete","incomplete_details":{"reason":"content_filter"},"output":[]}`))
		assert.ErrMsgContains(t, err, "content filter")
	})
}

func TestResponsesStream(t *testing.T) {
	p := NewResponsesProvider(ResponsesConfig{OpenAIConfig: OpenAIConfig{BaseURL: "https://api.commandcode.ai/provider/v1", Model: "deepseek/deepseek-v4.1-flash"}})
	var deltas []string
	emit := func(_ context.Context, ev agentcoreapi.StreamEvent) error {
		if ev.Kind == agentcoreapi.StreamEventTextDelta {
			deltas = append(deltas, ev.Delta)
		}
		return nil
	}
	item := func(i int, js string) string {
		return `data: {"type":"response.output_item.done","output_index":` + string(rune('0'+i)) + `,"item":` + js + `}`
	}
	t.Run("deltas emitted, result from the completed response", func(t *testing.T) {
		deltas = nil
		stream := strings.Join([]string{
			`event: response.created`, `data: {"type":"response.created","response":{"status":"in_progress"}}`, ``,
			`data: {"type":"response.output_text.delta","output_index":1,"delta":"Read"}`,
			`data: {"type":"response.output_text.delta","output_index":1,"delta":"ing."}`,
			`data: {"type":"response.completed","response":` + strings.ReplaceAll(responsesResult, "\n", "") + `}`,
		}, "\n")
		res, err := p.readResponsesStream(context.Background(), strings.NewReader(stream), emit)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, []string{"Read", "ing."}, deltas)
		assert.Eq(t, "Reading.", res.Content)
		assert.Eq(t, 1, len(res.ToolCalls))
	})
	t.Run("a terminal event without output uses the done items", func(t *testing.T) {
		stream := strings.Join([]string{
			item(1, `{"type":"function_call","call_id":"c2","name":"x","arguments":"{}"}`),
			item(0, `{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}`),
			`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":5,"output_tokens":1}}}`,
		}, "\n")
		res, err := p.readResponsesStream(context.Background(), strings.NewReader(stream), nil)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "ok", res.Content)
		assert.Eq(t, "c2", res.ToolCalls[0].CallID)
	})
	t.Run("cut before the terminal event but with every item finished", func(t *testing.T) {
		stream := strings.Join([]string{
			`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message"}}`,
			`data: {"type":"response.output_text.delta","output_index":0,"delta":"ok"}`,
			item(0, `{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}`),
			`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call"}}`,
			item(1, `{"type":"function_call","call_id":"c9","name":"x","arguments":"{}"}`),
		}, "\n")
		res, err := p.readResponsesStream(context.Background(), strings.NewReader(stream), nil)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "ok", res.Content)
		assert.Eq(t, "c9", res.ToolCalls[0].CallID)

		unfinished := strings.Join([]string{
			`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message"}}`,
			item(0, `{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}`),
			`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call"}}`,
			`data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"pa"}`,
		}, "\n")
		_, err = p.readResponsesStream(context.Background(), strings.NewReader(unfinished), nil)
		var broken *streamReadError
		assert.True(t, errors.As(err, &broken))
	})
	t.Run("broken and failed streams", func(t *testing.T) {
		_, err := p.readResponsesStream(context.Background(), strings.NewReader(`data: {"type":"response.output_text.delta","delta":"x"}`), nil)
		var broken *streamReadError
		assert.True(t, errors.As(err, &broken)) // requested again by retryStream
		_, err = p.readResponsesStream(context.Background(), strings.NewReader(`data: {"type":"error","message":"rate limited"}`), nil)
		assert.ErrMsg(t, err, "responses api error: rate limited")
		_, err = p.readResponsesStream(context.Background(), strings.NewReader(`data: {"type":"response.failed","response":{"status":"failed","error":{"message":"boom"}}}`), nil)
		assert.ErrMsg(t, err, "responses api error: boom")
	})
}

// TestResponsesReasoningRoundTrip: what a reply returned is stored with its
// first call (through JSON, as the session store does) and sent back first in
// that turn.
func TestResponsesReasoningRoundTrip(t *testing.T) {
	p := NewResponsesProvider(ResponsesConfig{OpenAIConfig: OpenAIConfig{BaseURL: "https://api.commandcode.ai/provider/v1", Model: "deepseek/deepseek-v4.1-flash", ReasoningEffort: "high"}})
	res, err := p.parseResponsesBody([]byte(responsesResult))
	assert.Require(t, assert.NoErr(t, err))
	raw, _ := json.Marshal(res.Metadata)
	var meta map[string]any
	assert.NoErr(t, json.Unmarshal(raw, &meta))
	call := res.ToolCalls[0]
	msgs := []agentcoreapi.Message{{Role: "user", Content: "go"},
		{Role: "assistant", Content: res.Content, Reasoning: res.Reasoning, ToolCall: &call, Metadata: meta},
		{Role: "tool", ToolCall: &call, ToolResult: &agentcoreapi.ToolResult{CallID: call.CallID, Content: "pkg a"}}}
	input := p.buildInput(msgs, true)
	assert.Eq(t, map[string]any{"type": "reasoning", "encrypted_content": "enc9",
		"summary": []any{map[string]any{"type": "summary_text", "text": "look at a.go"}}}, input[1])
}
