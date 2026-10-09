package chat

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gookit/color"
	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/internal/platform/tools/native"
	"github.com/inhere/suag/pkg/agentcore"
)

// turnEvents is one turn: commentary, a tool call and result, a plan, then the answer.
func turnEvents() []personal.Event {
	return []personal.Event{
		{Kind: "text_delta", Text: "looking"},
		{Kind: "message_stop"},
		{Kind: "tool_call", ToolName: "file_read", Text: `{"path":"a.go"}`},
		{Kind: "tool_result", ToolName: "file_read", Text: "package a\n\nfunc A() {}"},
		{Kind: "tool_call", ToolName: "shell", Text: `{"cmd":"false"}`},
		{Kind: "tool_result", ToolName: "shell", Text: "exit status 1", IsError: true},
		{Kind: "plan", Plan: &native.Plan{Explanation: "two steps", Steps: []native.PlanStep{
			{Step: "read", Status: "completed"}, {Step: "edit", Status: "in_progress"}, {Step: "test", Status: "pending"},
		}}},
		{Kind: "text_delta", Text: "done"},
		{Kind: "message_stop"},
	}
}

func runTurn(r *Renderer, elapsed time.Duration) {
	r.BeginTurn()
	for _, e := range turnEvents() {
		r.Handle(e)
	}
	r.Finish()
	r.Status(personal.RunResult{Usage: agentcore.Usage{InputTokens: 1200, OutputTokens: 34}}, elapsed)
}

func TestRendererPlainTurn(t *testing.T) {
	var out, errOut bytes.Buffer
	r := newRenderer(rendererOptions{Out: &out, Err: &errOut, Provider: "opencode", Model: "glm-5"})
	runTurn(r, 2340*time.Millisecond)

	want := strings.Join([]string{
		"looking",
		`⏺ file_read {"path":"a.go"}`,
		"  ⎿ ok: package a func A() {}",
		`⏺ shell {"cmd":"false"}`,
		"  ⎿ error: exit status 1",
		"⏺ plan two steps",
		"  ✔ read",
		"  ▶ edit",
		"  ○ test",
		"done",
		"opencode/glm-5 · in 1200 / out 34 tokens · 2.3s",
		"",
	}, "\n")
	assert.Eq(t, want, out.String())
	assert.StrNotContains(t, out.String(), "\x1b[")

	t.Run("prompt and error", func(t *testing.T) {
		assert.Eq(t, "> ", r.Prompt(false))
		assert.Eq(t, "> (可等待外部回复) ", r.Prompt(true))
		r.Error(errors.New("boom"))
		assert.Eq(t, "Error: boom\n", errOut.String())
	})
	t.Run("status without model", func(t *testing.T) {
		var b bytes.Buffer
		newRenderer(rendererOptions{Out: &b}).Status(personal.RunResult{}, 40*time.Millisecond)
		assert.Eq(t, "in 0 / out 0 tokens · 40ms\n", b.String())
	})
}

func TestRendererColoredTurn(t *testing.T) {
	// Force the color library on regardless of the test environment.
	color.ForceColor()
	t.Cleanup(func() { color.RevertColorLevel() })

	var out, errOut bytes.Buffer
	r := newRenderer(rendererOptions{Out: &out, Err: &errOut, Color: true, ErrColor: true, Provider: "p", Model: "m"})
	runTurn(r, time.Second)
	got := out.String()

	assert.StrContains(t, got, "\x1b[")
	// Tool line and status line are grey (90), failed results red (31).
	assert.StrContains(t, got, "\x1b[90m⏺ file_read {\"path\":\"a.go\"}\x1b[0m")
	assert.StrContains(t, got, "\x1b[31m  ⎿ error: exit status 1\x1b[0m")
	assert.StrContains(t, got, "\x1b[90mp/m · in 1200 / out 34 tokens · 1.0s\x1b[0m")
	assert.StrContains(t, got, "\x1b[32m  ✔ read\x1b[0m")
	assert.Eq(t, "\x1b[36m> \x1b[0m", r.Prompt(false))

	r.Error(errors.New("boom"))
	assert.StrContains(t, errOut.String(), "\x1b[31mError: boom\x1b[0m")
}

func TestColorEnabled(t *testing.T) {
	t.Run("tty without NO_COLOR", func(t *testing.T) {
		t.Setenv("NO_COLOR", "")
		assert.True(t, colorEnabled(true))
	})
	t.Run("NO_COLOR disables", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		assert.False(t, colorEnabled(true))
	})
	t.Run("not a tty", func(t *testing.T) {
		t.Setenv("NO_COLOR", "")
		assert.False(t, colorEnabled(false))
		assert.False(t, colorEnabledFor(&bytes.Buffer{}))
	})
}

func TestRendererExternalInput(t *testing.T) {
	t.Run("plain", func(t *testing.T) {
		var out bytes.Buffer
		newRenderer(rendererOptions{Out: &out}).External("[gofer web 回复] 你好\n第二行")
		assert.Eq(t, "[gofer web 回复] 你好\n第二行\n", out.String())
	})
	t.Run("colored per line", func(t *testing.T) {
		color.ForceColor()
		t.Cleanup(func() { color.RevertColorLevel() })
		var out bytes.Buffer
		newRenderer(rendererOptions{Out: &out, Color: true}).External("[来自 cli] a\nb")
		got := out.String()
		assert.Eq(t, 2, strings.Count(got, "\x1b[35m"))
		assert.StrContains(t, got, "[来自 cli] a")
		assert.StrContains(t, got, "b")
	})
}
