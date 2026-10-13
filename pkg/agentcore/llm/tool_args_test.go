package llm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"

	agentcoreapi "github.com/inhere/suga/pkg/agentcore/api"
)

func TestDecodeToolArguments(t *testing.T) {
	t.Run("raw control characters in strings are escaped", func(t *testing.T) {
		raw := "{\"path\": \"a.go\", \"new_string\": \"func f() {\n\treturn\r\n}\"}"
		args, err := decodeToolArguments(json.RawMessage(raw), "file_edit")
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "func f() {\n\treturn\r\n}", args["new_string"])
	})
	t.Run("raw control characters inside an encoded arguments string", func(t *testing.T) {
		inner := "{\"edits\": [{\"old_string\": \"\ta\", \"new_string\": \"\tb\\\"q\\\"\"}]}"
		encoded, err := json.Marshal(inner) // the outer string itself is valid JSON
		assert.NoErr(t, err)
		args, err := decodeToolArguments(encoded, "file_edit")
		assert.Require(t, assert.NoErr(t, err))
		edit := args["edits"].([]any)[0].(map[string]any)
		assert.Eq(t, "\ta", edit["old_string"])
		assert.Eq(t, "\tb\"q\"", edit["new_string"])
	})
	t.Run("whitespace between tokens is untouched", func(t *testing.T) {
		args, err := decodeToolArguments(json.RawMessage("{\n\t\"a\": \"x\"\n}"), "t")
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "x", args["a"])
	})
	t.Run("other syntax errors keep the strict error", func(t *testing.T) {
		_, err := decodeToolArguments(json.RawMessage("{\"a\": \"x\t\""), "t")
		assert.ErrMsgContains(t, err, `decode tool call "t" arguments`)
		_, err = decodeToolArguments(json.RawMessage(`["a"]`), "t")
		assert.ErrMsgContains(t, err, "must be a JSON object")
	})
}

func TestNewToolCallKeepsUndecodableArguments(t *testing.T) {
	t.Run("valid arguments", func(t *testing.T) {
		call := newToolCall("c1", "file_read", json.RawMessage(`{"path":"a.go"}`))
		assert.Eq(t, "a.go", call.Arguments["path"])
		assert.Eq(t, "", call.ArgumentsError)
	})
	t.Run("an unescaped quote is not repairable", func(t *testing.T) {
		raw := "{\"path\": \"a.go\", \"new_string\": \"fmt.Println(\"x\")\n\"}"
		call := newToolCall("c1", "file_edit", json.RawMessage(raw))
		assert.Eq(t, "c1", call.CallID)
		assert.Eq(t, "file_edit", call.Name)
		assert.NotNil(t, call.Arguments) // sent back as {} in the history
		assert.Empty(t, call.Arguments)
		assert.StrContains(t, call.ArgumentsError, `decode tool call "file_edit" arguments`)
	})
}

func TestStreamKeepsUndecodableToolCall(t *testing.T) {
	server := sseServer(t, 200, strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"file_edit","arguments":"{\"path\":\"a.go\",\"x\":\"q\"q\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`, ``,
	}, "\n"), nil)
	req := &agentcoreapi.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "hi"}}}
	res, err := NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "m"}).Stream(context.Background(), req, nil)
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, 1, len(res.ToolCalls))
	assert.StrContains(t, res.ToolCalls[0].ArgumentsError, "decode tool call")
	assert.Eq(t, agentcoreapi.FinishToolCalls, res.FinishReason)
}

func TestTruncatedResponseMarksToolCalls(t *testing.T) {
	req := &agentcoreapi.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "hi"}}}
	t.Run("openai stream finish_reason length", func(t *testing.T) {
		server := sseServer(t, 200, strings.Join([]string{
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"file_write","arguments":"{\"path\":\"a.go\",\"content\":\"pack"}}]},"finish_reason":"length"}]}`,
			`data: [DONE]`, ``,
		}, "\n"), nil)
		res, err := NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "m"}).Stream(context.Background(), req, nil)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, truncatedCallError, res.ToolCalls[0].ArgumentsError)
	})
	t.Run("a complete call of a cut-off response is not run either", func(t *testing.T) {
		res, err := NewOpenAIProvider(OpenAIConfig{}).parseResponse([]byte(`{"choices":[{"message":{"tool_calls":[{"id":"c1","function":{"name":"file_read","arguments":"{\"path\":\"a.go\"}"}}]},"finish_reason":"length"}]}`))
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, truncatedCallError, res.ToolCalls[0].ArgumentsError)
	})
	t.Run("claude stop_reason max_tokens", func(t *testing.T) {
		res, err := NewClaudeProvider(ClaudeConfig{}).parseResponse([]byte(`{"content":[{"type":"tool_use","id":"c1","name":"file_read","input":{"path":"a.go"}}],"stop_reason":"max_tokens","usage":{"input_tokens":1,"output_tokens":1}}`))
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, truncatedCallError, res.ToolCalls[0].ArgumentsError)
	})
	t.Run("a normal stop leaves calls alone", func(t *testing.T) {
		res, err := NewOpenAIProvider(OpenAIConfig{}).parseResponse([]byte(`{"choices":[{"message":{"tool_calls":[{"id":"c1","function":{"name":"file_read","arguments":"{\"path\":\"a.go\"}"}}]},"finish_reason":"tool_calls"}]}`))
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "", res.ToolCalls[0].ArgumentsError)
	})
}

func TestCachedTokensFallback(t *testing.T) {
	res, err := NewOpenAIProvider(OpenAIConfig{}).parseResponse([]byte(`{"choices":[{"message":{"content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":20}}`))
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, 80, res.Usage.CacheReadTokens) // DeepSeek native field
	res, err = NewOpenAIProvider(OpenAIConfig{}).parseResponse([]byte(`{"choices":[{"message":{"content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105,"prompt_tokens_details":{"cached_tokens":64},"prompt_cache_hit_tokens":80}}`))
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, 64, res.Usage.CacheReadTokens) // the OpenAI field wins
	server := sseServer(t, 200, strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"hi"},"finish_reason":"stop"}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11,"prompt_cache_hit_tokens":7}}`,
		`data: [DONE]`, ``,
	}, "\n"), nil)
	req := &agentcoreapi.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "hi"}}}
	sres, err := NewOpenAIProvider(OpenAIConfig{APIKey: "k", BaseURL: server.URL, Model: "m"}).Stream(context.Background(), req, nil)
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, 7, sres.Usage.CacheReadTokens)
}
