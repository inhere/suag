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
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/tools/native"
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

	// Each model message starts with the ▣ marker; tool and plan lines do not.
	want := strings.Join([]string{
		"▣ looking",
		`⏺ file_read {"path":"a.go"}`,
		"  ⎿ ok: package a func A() {}",
		`⏺ shell {"cmd":"false"}`,
		"  ⎿ error: exit status 1",
		"⏺ plan two steps",
		"  ✔ read",
		"  ▶ edit",
		"  ○ test",
		"▣ done",
		"opencode/glm-5 · in 1200 / out 34 tokens · 2.3s",
		"",
	}, "\n")
	assert.Eq(t, want, out.String())
	assert.StrNotContains(t, out.String(), "\x1b[")

	t.Run("prompt and error", func(t *testing.T) {
		assert.Eq(t, "❯ ", r.Prompt(false))
		assert.Eq(t, "❯ (可等待外部回复) ", r.Prompt(true))
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
	assert.Eq(t, "\x1b[36m❯ \x1b[0m", r.Prompt(false))

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

func TestBannerAndExitSummary(t *testing.T) {
	t.Run("banner shows the model", func(t *testing.T) {
		var out bytes.Buffer
		newRenderer(rendererOptions{Out: &out, Provider: "opencode-go", Model: "deepseek-v4-flash"}).Banner()
		assert.StrContains(t, out.String(), "Suag chat · opencode-go/deepseek-v4-flash\n")
	})
	t.Run("banner adds the thinking level and the context window", func(t *testing.T) {
		var out bytes.Buffer
		newRenderer(rendererOptions{Out: &out, Provider: "command-code", Model: "deepseek-v4.1-flash",
			Thinking: "high", ContextWindow: 950000}).Banner()
		assert.StrContains(t, out.String(), "Suag chat · command-code/deepseek-v4.1-flash · thinking high · ctx 950k\n")
	})
	t.Run("token counts", func(t *testing.T) {
		for n, want := range map[int]string{512: "512", 128000: "128k", 950000: "950k", 1000000: "1M", 1048576: "1.05M", 200500: "200.5k"} {
			assert.Eq(t, want, formatTokens(n), n)
		}
	})
	t.Run("exit summary", func(t *testing.T) {
		var s chatStats
		s.add(personal.RunResult{Usage: agentcore.Usage{InputTokens: 100, OutputTokens: 20}})
		s.add(personal.RunResult{Usage: agentcore.Usage{InputTokens: 50, OutputTokens: 5},
			SessionUsage: agentcore.Usage{InputTokens: 900, OutputTokens: 80, TotalTokens: 980}})
		var out bytes.Buffer
		s.print(&out, "sess_x", 125*time.Second)
		assert.Eq(t, "\n本次 2 轮 · in 150 / out 25 tokens（会话累计 in 900 / out 80） · 运行 2m5s\n续接：suag chat --resume sess_x\n", out.String())
	})
	t.Run("no session no summary", func(t *testing.T) {
		var out bytes.Buffer
		chatStats{}.print(&out, "", time.Second)
		assert.Eq(t, "", out.String())
	})
	t.Run("elapsed formats", func(t *testing.T) {
		assert.Eq(t, "40ms", FormatElapsed(40*time.Millisecond))
		assert.Eq(t, "2.5s", FormatElapsed(2500*time.Millisecond))
		assert.Eq(t, "1h2m3s", FormatElapsed(time.Hour+2*time.Minute+3*time.Second))
	})
}

func TestRendererBannerToolLine(t *testing.T) {
	t.Run("read only points at tools.allow_write", func(t *testing.T) {
		var out bytes.Buffer
		newRenderer(rendererOptions{Out: &out, Model: "m"}).Banner()
		assert.StrContains(t, out.String(), "工具：只读")
		assert.StrContains(t, out.String(), "tools.allow_write: true")
	})
	t.Run("write tools on", func(t *testing.T) {
		var out bytes.Buffer
		newRenderer(rendererOptions{Out: &out, Model: "m", WriteTools: true}).Banner()
		assert.StrContains(t, out.String(), "执行 shell")
		assert.StrNotContains(t, out.String(), "工具：只读")
	})
}

// TestRendererMemoryWrites: successful memory writes of the turn are listed
// before the status line; failed ones and other tools are not.
func TestRendererMemoryWrites(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(rendererOptions{Out: &out, Err: &out, Model: "m"})
	r.BeginTurn()
	for _, e := range []personal.Event{
		{Kind: "tool_result", ToolName: "memory", Text: "Saved memory use-tabs (project): Use tabs\nkind: feedback"},
		{Kind: "tool_result", ToolName: "memory", Text: "Updated memory name (user): My name"},
		{Kind: "tool_result", ToolName: "memory", Text: "Removed memory old (project): Old note"},
		{Kind: "tool_result", ToolName: "memory", Text: "Saved memory x (project): denied", IsError: true},
		{Kind: "tool_result", ToolName: "file_read", Text: "Saved memory y (project): not memory"},
	} {
		r.Handle(e)
	}
	r.Status(personal.RunResult{}, time.Second)
	assert.StrContains(t, out.String(), "已记住：Use tabs（project）\n已更新：My name（user）\n已删除：Old note（project）\nm · in 0")
	assert.NotContains(t, out.String(), "denied（")
	assert.NotContains(t, out.String(), "not memory（")

	t.Run("the next turn starts empty", func(t *testing.T) {
		out.Reset()
		r.BeginTurn()
		r.Status(personal.RunResult{}, time.Second)
		assert.NotContains(t, out.String(), "已记住")
	})
}

// TestRendererAgentProgress: a sub-agent's tool calls show indented under the
// agent call, its result as usual.
func TestRendererAgentProgress(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(rendererOptions{Out: &out, Err: &out})
	r.Handle(personal.Event{Kind: "tool_call", ToolName: "agent", Text: `{"description":"find Foo"}`})
	r.Handle(personal.Event{Kind: "agent_progress", Agent: "find Foo", ToolName: "file_grep", Text: `{"pattern":"Foo"}`})
	r.Handle(personal.Event{Kind: "tool_result", ToolName: "agent", Text: "Foo is in a.go:3"})
	assert.Eq(t, strings.Join([]string{
		`⏺ agent {"description":"find Foo"}`,
		`  · file_grep {"pattern":"Foo"}`,
		"  ⎿ ok: Foo is in a.go:3",
		"",
	}, "\n"), out.String())
}
