package output

import (
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal"
)

func TestEventLineToolEvents(t *testing.T) {
	t.Run("tool_call carries name and args summary", func(t *testing.T) {
		line, ok := EventLine(personal.Event{Kind: "tool_call", SessionID: "s", RunID: "r", ToolName: "file_read", Text: `{"path":"a.txt"}`})
		assert.True(t, ok)
		assert.Eq(t, map[string]any{
			"session_id": "s", "run_id": "r", "type": "tool_call", "name": "file_read", "args": `{"path":"a.txt"}`,
		}, line)
	})

	t.Run("tool_result summary is redacted before truncation", func(t *testing.T) {
		secret := "sk-abcdefghijklmnop"
		// The secret sits right at the truncation boundary: truncating first would leak a prefix.
		content := strings.Repeat("x", toolSummaryLimit-12) + " api_key=" + secret + " tail"
		cut, _ := EventLine(personal.Event{Kind: "tool_result", Text: content})
		assert.StrNotContains(t, cut["summary"].(string), "sk-")
		content = "api_key=" + secret + " " + strings.Repeat("x", 300)
		line, ok := EventLine(personal.Event{Kind: "tool_result", ToolName: "shell_exec", Text: content, IsError: true})
		assert.True(t, ok)
		assert.Eq(t, "tool_result", line["type"])
		assert.Eq(t, "shell_exec", line["name"])
		assert.Eq(t, true, line["is_error"])
		summary := line["summary"].(string)
		assert.StrNotContains(t, summary, "sk-")
		assert.StrContains(t, summary, "api_key=[REDACTED] xxx")
		assert.True(t, len([]rune(summary)) <= toolSummaryLimit)
	})

	t.Run("long result is truncated and collapsed to one line", func(t *testing.T) {
		got := ToolResultSummary("a\n\nb " + strings.Repeat("y", 500))
		assert.StrNotContains(t, got, "\n")
		assert.Eq(t, toolSummaryLimit, len([]rune(got)))
		assert.True(t, strings.HasSuffix(got, "…"))
	})
}
