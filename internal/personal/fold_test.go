package personal

import (
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/llm"
)

// foldSteps builds a user message plus n assistant tool steps (each with
// reasoning and a long file_write) and their tool results.
func foldSteps(n int) []agentcore.Message {
	msgs := []agentcore.Message{{Role: "user", Content: "task"}}
	long := strings.Repeat("x", foldArgChars+1)
	for i := 0; i < n; i++ {
		call := &agentcore.ToolCall{CallID: "c", Name: "file_write", Arguments: map[string]any{
			"path": "a.go", "content": long,
			"edits": []any{map[string]any{"new_string": long, "old_string": "short"}},
		}}
		msgs = append(msgs,
			agentcore.Message{Role: "assistant", Reasoning: "think", ToolCall: call},
			agentcore.Message{Role: "tool", Content: "ok", ToolCall: call})
	}
	return msgs
}

func folded(m agentcore.Message) bool { return m.Reasoning == "" }

func TestFoldHistory(t *testing.T) {
	t.Run("off or too few steps leaves the view alone", func(t *testing.T) {
		msgs := foldSteps(5)
		assert.Eq(t, msgs, foldHistory(msgs, 0, 4))
		assert.Eq(t, msgs, foldHistory(msgs, 4, 4)) // 5-4 = 1 step past keep, below one batch
	})
	t.Run("the boundary moves in batches", func(t *testing.T) {
		count := func(n int) int {
			c := 0
			for _, m := range foldHistory(foldSteps(n), 4, 4) {
				if m.Role == "assistant" && folded(m) {
					c++
				}
			}
			return c
		}
		assert.Eq(t, 0, count(7))
		assert.Eq(t, 4, count(8))
		assert.Eq(t, 4, count(11)) // same boundary: same request prefix
		assert.Eq(t, 8, count(12))
	})
	t.Run("folded steps lose reasoning and long arguments only", func(t *testing.T) {
		msgs := foldSteps(9)
		out := foldHistory(msgs, 4, 4)
		first := out[1]
		assert.Eq(t, "", first.Reasoning)
		assert.Eq(t, "a.go", first.ToolCall.Arguments["path"])
		assert.StrContains(t, first.ToolCall.Arguments["content"].(string), "[folded: 1001 chars")
		edit := first.ToolCall.Arguments["edits"].([]any)[0].(map[string]any)
		assert.StrContains(t, edit["new_string"].(string), "[folded:")
		assert.Eq(t, "short", edit["old_string"])
		last := out[len(out)-2]
		assert.Eq(t, "think", last.Reasoning)
		assert.Eq(t, foldArgChars+1, len(last.ToolCall.Arguments["content"].(string)))
		assert.Eq(t, true, first.Metadata[llm.ReasoningFoldedMetaKey]) // keeps thinking mode on the wire
		assert.Nil(t, last.Metadata)
		// The stored history is untouched.
		assert.Eq(t, "think", msgs[1].Reasoning)
		assert.Nil(t, msgs[1].Metadata)
		assert.Eq(t, foldArgChars+1, len(msgs[1].ToolCall.Arguments["content"].(string)))
	})
	t.Run("steps without reasoning get no mark", func(t *testing.T) {
		msgs := foldSteps(9)
		for i := range msgs {
			msgs[i].Reasoning = ""
		}
		for _, m := range foldHistory(msgs, 4, 4) {
			assert.Nil(t, m.Metadata)
		}
	})
}
