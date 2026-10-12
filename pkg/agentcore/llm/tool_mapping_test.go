package llm

import (
	"encoding/json"
	"testing"

	"github.com/gookit/goutil/x/assert"
	agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"
)

// toolPair mirrors agentcore persistToolTranscript: assistant{ToolCall} then
// tool{Content, ToolCall, ToolResult}.
func toolPair(id, name, out string, isError bool) []agentcoreapi.Message {
	call := &agentcoreapi.ToolCall{CallID: id, Name: name, Arguments: map[string]any{"k": "v"}}
	return []agentcoreapi.Message{
		{Role: "assistant", ToolCall: call},
		{Role: "tool", Content: out, ToolCall: call,
			ToolResult: &agentcoreapi.ToolResult{CallID: id, Name: name, Content: out, IsError: isError}},
	}
}

func toJSON(t *testing.T, v any) string {
	b, err := json.Marshal(v)
	assert.NoErr(t, err)
	return string(b)
}

func TestClaudeProvider_BuildRequestBody_ToolTranscript(t *testing.T) {
	p := NewClaudeProvider(ClaudeConfig{})
	build := func(ms ...agentcoreapi.Message) []map[string]any {
		return p.buildRequestBody(&agentcoreapi.GenerateRequest{Messages: ms}, "m")["messages"].([]map[string]any)
	}
	user := agentcoreapi.Message{Role: "user", Content: "go"}

	t.Run("single call", func(t *testing.T) {
		msgs := build(append([]agentcoreapi.Message{user}, toolPair("c1", "ls", "ok", false)...)...)
		assert.Eq(t, 3, len(msgs))
		assert.Eq(t, `{"content":[{"id":"c1","input":{"k":"v"},"name":"ls","type":"tool_use"}],"role":"assistant"}`, toJSON(t, msgs[1]))
		assert.Eq(t, `{"content":[{"content":"ok","tool_use_id":"c1","type":"tool_result"}],"role":"user"}`, toJSON(t, msgs[2]))
	})

	t.Run("parallel calls alternate roles", func(t *testing.T) {
		ms := append([]agentcoreapi.Message{user}, toolPair("c1", "a", "r1", false)...)
		ms = append(ms, toolPair("c2", "b", "r2", false)...)
		msgs := build(ms...)
		assert.Eq(t, 5, len(msgs))
		for i, want := range []string{"user", "assistant", "user", "assistant", "user"} {
			assert.Eq(t, want, msgs[i]["role"])
		}
		assert.Eq(t, 1, len(msgs[4]["content"].([]map[string]any)))
	})

	t.Run("consecutive blocks merge", func(t *testing.T) {
		a := toolPair("c1", "a", "r1", false)
		b := toolPair("c2", "b", "r2", false)
		msgs := build(user, a[0], b[0], a[1], b[1])
		assert.Eq(t, 3, len(msgs))
		assert.Eq(t, 2, len(msgs[1]["content"].([]map[string]any)))
		assert.Eq(t, 2, len(msgs[2]["content"].([]map[string]any)))
	})

	t.Run("assistant text before call", func(t *testing.T) {
		// Text-only assistant messages keep string content and stay separate.
		msgs := build(append([]agentcoreapi.Message{user, {Role: "assistant", Content: "thinking"}}, toolPair("c1", "a", "r", false)...)...)
		assert.Eq(t, 4, len(msgs))
		assert.Eq(t, "thinking", msgs[1]["content"])
	})

	t.Run("text with call", func(t *testing.T) {
		pair := toolPair("c1", "a", "r", false)
		pair[0].Content = "checking"
		msgs := build(append([]agentcoreapi.Message{user}, pair...)...)
		assert.Eq(t, 3, len(msgs))
		assert.Eq(t, `{"content":[{"text":"checking","type":"text"},{"id":"c1","input":{"k":"v"},"name":"a","type":"tool_use"}],"role":"assistant"}`, toJSON(t, msgs[1]))
	})

	t.Run("error result", func(t *testing.T) {
		msgs := build(append([]agentcoreapi.Message{user}, toolPair("c1", "a", "boom", true)...)...)
		assert.Eq(t, `{"content":[{"content":"boom","is_error":true,"tool_use_id":"c1","type":"tool_result"}],"role":"user"}`, toJSON(t, msgs[2]))
	})
}

func TestOpenAIProvider_BuildRequestBody_ToolTranscript(t *testing.T) {
	p := NewOpenAIProvider(OpenAIConfig{})
	build := func(ms ...agentcoreapi.Message) []map[string]any {
		return p.buildRequestBody(&agentcoreapi.GenerateRequest{Messages: ms}, "m", false)["messages"].([]map[string]any)
	}
	user := agentcoreapi.Message{Role: "user", Content: "go"}

	t.Run("single call", func(t *testing.T) {
		msgs := build(append([]agentcoreapi.Message{user}, toolPair("c1", "ls", "ok", false)...)...)
		assert.Eq(t, 3, len(msgs))
		assert.Eq(t, "assistant", msgs[1]["role"])
		assert.Eq(t, 1, len(msgs[1]["tool_calls"].([]map[string]any)))
		// tool message must not carry tool_calls
		assert.Eq(t, `{"content":"ok","role":"tool","tool_call_id":"c1"}`, toJSON(t, msgs[2]))
	})

	t.Run("text with call", func(t *testing.T) {
		pair := toolPair("c1", "a", "r", false)
		pair[0].Content = "checking"
		msgs := build(append([]agentcoreapi.Message{user}, pair...)...)
		assert.Eq(t, "checking", msgs[1]["content"])
		assert.Eq(t, 1, len(msgs[1]["tool_calls"].([]map[string]any)))
	})

	t.Run("consecutive calls merge", func(t *testing.T) {
		a := toolPair("c1", "a", "r1", false)
		b := toolPair("c2", "b", "r2", false)
		msgs := build(user, a[0], b[0], a[1], b[1])
		assert.Eq(t, 4, len(msgs))
		assert.Eq(t, 2, len(msgs[1]["tool_calls"].([]map[string]any)))
		assert.Eq(t, "tool", msgs[2]["role"])
		assert.Eq(t, "tool", msgs[3]["role"])
	})
}

// Thinking-mode APIs require each earlier assistant message's reasoning_content
// back on requests that carry tools; it is never added to user or tool messages.
func TestOpenAIProvider_ReasoningContentRoundTrip(t *testing.T) {
	p := NewOpenAIProvider(OpenAIConfig{})
	t.Run("parse", func(t *testing.T) {
		res, err := p.parseResponse([]byte(`{"choices":[{"message":{"content":"","reasoning_content":"think","tool_calls":[{"id":"c1","function":{"name":"ls","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`))
		assert.NoErr(t, err)
		assert.Eq(t, "think", res.Reasoning)
		assert.Eq(t, agentcoreapi.FinishToolCalls, res.FinishReason)
	})
	t.Run("echo on assistant messages", func(t *testing.T) {
		pair := toolPair("c1", "ls", "ok", false)
		pair[0].Reasoning = "think-1"
		ms := append([]agentcoreapi.Message{{Role: "user", Content: "go"}}, pair...)
		ms = append(ms, agentcoreapi.Message{Role: "assistant", Content: "done", Reasoning: "think-2"}, agentcoreapi.Message{Role: "user", Content: "next"})
		msgs := p.buildRequestBody(&agentcoreapi.GenerateRequest{Messages: ms}, "m", false)["messages"].([]map[string]any)
		assert.Eq(t, 5, len(msgs))
		assert.Eq(t, "think-1", msgs[1]["reasoning_content"])
		// A plain answer does not carry its reasoning back (tool-call messages only).
		for _, i := range []int{0, 2, 3, 4} {
			_, has := msgs[i]["reasoning_content"]
			assert.Eq(t, false, has)
		}
	})
	t.Run("empty reasoning still echoed in a thinking session", func(t *testing.T) {
		first := toolPair("c1", "ls", "ok", false)
		first[0].Reasoning = "think-1"
		second := toolPair("c2", "ls", "ok", false) // a step that produced no reasoning
		ms := append([]agentcoreapi.Message{{Role: "user", Content: "go"}}, first...)
		ms = append(ms, second...)
		msgs := p.buildRequestBody(&agentcoreapi.GenerateRequest{Messages: ms}, "m", true)["messages"].([]map[string]any)
		assert.Eq(t, "think-1", msgs[1]["reasoning_content"])
		got, has := msgs[3]["reasoning_content"]
		assert.Eq(t, true, has)
		assert.Eq(t, "", got)
		_, has = msgs[2]["reasoning_content"] // tool messages never carry it
		assert.Eq(t, false, has)
	})
	t.Run("a folded step keeps the session in thinking mode", func(t *testing.T) {
		// History folding removed the only reasoning; the remaining steps of a
		// parallel batch never had any. Every assistant message still echoes it.
		folded := toolPair("c1", "ls", "ok", false)
		folded[0].Metadata = map[string]any{ReasoningFoldedMetaKey: true}
		rest := toolPair("c2", "ls", "ok", false)
		ms := append([]agentcoreapi.Message{{Role: "user", Content: "go"}}, folded...)
		ms = append(ms, rest...)
		msgs := p.buildRequestBody(&agentcoreapi.GenerateRequest{Messages: ms}, "m", true)["messages"].([]map[string]any)
		for _, i := range []int{1, 3} {
			got, has := msgs[i]["reasoning_content"]
			assert.Eq(t, true, has)
			assert.Eq(t, "", got)
		}
	})
	t.Run("absent when empty", func(t *testing.T) {
		msgs := p.buildRequestBody(&agentcoreapi.GenerateRequest{Messages: []agentcoreapi.Message{{Role: "user", Content: "go"}, {Role: "assistant", Content: "hi"}}}, "m", false)["messages"].([]map[string]any)
		_, has := msgs[1]["reasoning_content"]
		assert.Eq(t, false, has)
	})
}
