package chat

import (
	"bytes"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/diffview"
	"github.com/inhere/suga/internal/personal"
	"github.com/inhere/suga/pkg/agentcore"
)

func TestRendererCleansExternalTerminalText(t *testing.T) {
	t.Run("tool call and result previews", func(t *testing.T) {
		var out bytes.Buffer
		r := newRenderer(rendererOptions{Out: &out})
		r.Handle(personal.Event{Kind: "tool_call", ToolName: "shell_exec",
			Text: "echo before\x1b[2Jafter"})
		r.Handle(personal.Event{Kind: "tool_result", ToolName: "shell_exec",
			Text: "result\x1b]52;c;clipboard\a after"})
		assert.StrContains(t, out.String(), "beforeafter")
		assert.StrContains(t, out.String(), "result after")
		assert.StrNotContains(t, out.String(), "\x1b")
	})

	t.Run("diff preview", func(t *testing.T) {
		var out bytes.Buffer
		oldText := "old\n"
		newText := "new\x1b[2J\n"
		r := newRenderer(rendererOptions{Out: &out, Diff: true})
		r.Handle(personal.Event{Kind: "tool_call", ToolName: "file_edit", Text: "{}",
			Change: &diffview.Change{Rel: "file.txt", Tool: diffview.ToolEdit, Old: &oldText, New: &newText}})
		r.Handle(personal.Event{Kind: "tool_result", ToolName: "file_edit", Text: "edited"})
		assert.StrContains(t, out.String(), "new")
		assert.StrNotContains(t, out.String(), "\x1b")
	})

	t.Run("streamed answer and markdown", func(t *testing.T) {
		var out bytes.Buffer
		r := newRenderer(rendererOptions{Out: &out, Color: true, Markdown: true})
		r.Handle(personal.Event{Kind: string(agentcore.StreamEventTextDelta), Text: "answer\x1b]52;c;clip"})
		r.Handle(personal.Event{Kind: string(agentcore.StreamEventTextDelta), Text: "board\a\x1b[2"})
		r.Handle(personal.Event{Kind: string(agentcore.StreamEventTextDelta), Text: "J\n**normal**"})
		r.Handle(personal.Event{Kind: string(agentcore.StreamEventMessageStop)})
		r.Finish()
		assert.StrContains(t, out.String(), "answer\n")
		assert.StrContains(t, out.String(), "normal")
		assert.StrNotContains(t, out.String(), "\x1b]52")
		assert.StrNotContains(t, out.String(), "\x1b[2J")
	})
}
