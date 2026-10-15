package personal

import (
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore"
)

func TestTokensOfText(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want int
	}{
		{"empty", "", 0},
		{"four ASCII bytes are one token", "abcd", 1},
		{"ASCII rounds up", "abcde", 2},
		{"one CJK character is one token", "好", 1},
		{"three CJK characters", "上下文", 3},
		{"mixed: two narrow bytes and a wide rune", "ab好", 2},
		{"fullwidth punctuation is wide", "，", 1},
		{"an emoji is wide", "😀", 1},
		{"newlines count as narrow bytes", "\n\n\n\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Eq(t, tc.want, tokensOfText(tc.text))
		})
	}
}

func TestTokensOfMessages(t *testing.T) {
	t.Run("content and reasoning both occupy the window", func(t *testing.T) {
		ms := []agentcore.Message{{Role: "assistant", Content: "abcd", Reasoning: "efgh"}}
		assert.Eq(t, 2, tokensOfMessages(ms))
	})

	t.Run("a tool result is counted once", func(t *testing.T) {
		ms := []agentcore.Message{{Role: "tool",
			Content:    "abcd",
			ToolResult: &agentcore.ToolResult{Name: "t", Content: "abcdefghijkl"}}}
		assert.Eq(t, 3, tokensOfMessages(ms))
	})

	t.Run("a tool call counts its name and arguments", func(t *testing.T) {
		ms := []agentcore.Message{{Role: "assistant", ToolCall: &agentcore.ToolCall{
			Name: "file_read", Arguments: map[string]any{"path": "abcd"}}}}
		// name (9 bytes -> 3 tokens, rounded up) + {"path":"abcd"} (15 bytes -> 4)
		assert.Eq(t, 7, tokensOfMessages(ms))
	})

	t.Run("CJK content is not read as bytes/4", func(t *testing.T) {
		ms := []agentcore.Message{{Role: "user", Content: "上下文的估算"}}
		assert.Eq(t, 6, tokensOfMessages(ms))
		assert.Eq(t, 4, charsOfMessages(ms)/4) // the old estimate read it low (18 bytes)
	})
}
