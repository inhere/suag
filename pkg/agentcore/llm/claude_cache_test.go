package llm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"
)

func cacheTestReq() *agentcoreapi.GenerateRequest {
	return &agentcoreapi.GenerateRequest{
		System: "sys",
		Messages: []agentcoreapi.Message{
			{Role: "user", Content: "segment ctx", Metadata: map[string]any{SegmentContextMetaKey: true}},
			{Role: "user", Content: "hello"},
			{Role: "assistant", ToolCall: &agentcoreapi.ToolCall{CallID: "c1", Name: "t", Arguments: map[string]any{}}},
			{Role: "tool", ToolCall: &agentcoreapi.ToolCall{CallID: "c1"}, ToolResult: &agentcoreapi.ToolResult{CallID: "c1", Content: "ok"}},
		},
	}
}

func marshalBody(t *testing.T, p *ClaudeProvider, req *agentcoreapi.GenerateRequest) map[string]any {
	t.Helper()
	data, err := json.Marshal(p.buildRequestBody(req, "m"))
	assert.NoErr(t, err)
	var out map[string]any
	assert.NoErr(t, json.Unmarshal(data, &out))
	return out
}

func TestClaudePromptCache(t *testing.T) {
	t.Run("off: system stays a string and no cache_control", func(t *testing.T) {
		data, _ := json.Marshal(NewClaudeProvider(ClaudeConfig{}).buildRequestBody(cacheTestReq(), "m"))
		assert.False(t, strings.Contains(string(data), "cache_control"))
		body := marshalBody(t, NewClaudeProvider(ClaudeConfig{}), cacheTestReq())
		assert.Eq(t, "sys", body["system"])
		// string content stays a string when caching is off
		assert.Eq(t, "segment ctx", body["messages"].([]any)[0].(map[string]any)["content"])
	})

	t.Run("on: breakpoints on segment message and last message", func(t *testing.T) {
		body := marshalBody(t, NewClaudeProvider(ClaudeConfig{PromptCache: true}), cacheTestReq())
		// with caching on, system is a block array
		sys, ok := body["system"].([]any)
		assert.Require(t, assert.True(t, ok))
		assert.Eq(t, "sys", sys[0].(map[string]any)["text"])
		msgs := body["messages"].([]any)
		// segment message: string content converted to a text block with the breakpoint
		seg := msgs[0].(map[string]any)["content"].([]any)
		assert.Eq(t, 1, len(seg))
		b := seg[0].(map[string]any)
		assert.Eq(t, "text", b["type"])
		assert.Eq(t, "segment ctx", b["text"])
		assert.Eq(t, map[string]any{"type": "ephemeral"}, b["cache_control"])
		// the middle user string message is untouched
		assert.Eq(t, "hello", msgs[1].(map[string]any)["content"])
		// last message: tool_result block carries the breakpoint
		lastBlocks := msgs[len(msgs)-1].(map[string]any)["content"].([]any)
		assert.Eq(t, "tool_result", lastBlocks[len(lastBlocks)-1].(map[string]any)["type"])
		assert.NotNil(t, lastBlocks[len(lastBlocks)-1].(map[string]any)["cache_control"])
		data, _ := json.Marshal(body)
		assert.Eq(t, 2, strings.Count(string(data), "cache_control"))
	})

	t.Run("on: segment message is also last yields a single breakpoint", func(t *testing.T) {
		req := &agentcoreapi.GenerateRequest{Messages: []agentcoreapi.Message{
			{Role: "user", Content: "only", Metadata: map[string]any{SegmentContextMetaKey: true}},
		}}
		data, _ := json.Marshal(NewClaudeProvider(ClaudeConfig{PromptCache: true}).buildRequestBody(req, "m"))
		assert.Eq(t, 1, strings.Count(string(data), "cache_control"))
	})

	t.Run("on: no segment message marks only the last", func(t *testing.T) {
		req := cacheTestReq()
		req.Messages = req.Messages[1:]
		data, _ := json.Marshal(NewClaudeProvider(ClaudeConfig{PromptCache: true}).buildRequestBody(req, "m"))
		assert.Eq(t, 1, strings.Count(string(data), "cache_control"))
	})

	t.Run("on: empty request does not panic", func(t *testing.T) {
		NewClaudeProvider(ClaudeConfig{PromptCache: true}).buildRequestBody(&agentcoreapi.GenerateRequest{}, "m")
	})
}
