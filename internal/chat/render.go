package chat

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gookit/cliui/show/symbols"
	"github.com/gookit/color"
	"github.com/inhere/suga/internal/output"
	"github.com/inhere/suga/internal/personal"
	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/redact"
	"github.com/inhere/suga/pkg/agentcore/tools"
	"github.com/inhere/suga/pkg/agentcore/tools/native"
	"golang.org/x/term"
)

const (
	// resultSummaryLimit caps the one-line tool result summary, in runes.
	resultSummaryLimit = 100
	// callArgsLimit caps the tool_call argument text on screen, in runes.
	callArgsLimit = 120

	promptText        = "❯ "
	promptWaitingText = "❯ (可等待外部回复) "
)

// rendererOptions configures a Renderer. Color switches ANSI styling for Out
// and ErrColor for Err; both false yields plain text (symbols stay).
type rendererOptions struct {
	Out, Err        io.Writer
	Color, ErrColor bool
	// Provider and Model label the per-turn status line; either may be empty.
	Provider, Model string
	// Thinking and ContextWindow follow the model in the banner when set.
	Thinking      string
	ContextWindow int
	// WriteTools selects the banner's tool line (write and shell on or off).
	WriteTools bool
	// Markdown renders the model's text as terminal markdown (config
	// chat.markdown); it only applies to a colored terminal.
	Markdown bool
	// Spinner shows the "思考中…" waiting line while the model is asked for its
	// next message (config chat.spinner, on by default). Like Markdown it needs
	// a colored terminal: a pipe, a redirected output or NO_COLOR keeps it off.
	Spinner bool
	// spinnerNow and spinnerTicks drive that waiting line in tests (nil: the
	// real clock and a time.Ticker).
	spinnerNow   func() time.Time
	spinnerTicks func(time.Duration) (<-chan time.Time, func())
}

// Renderer owns all terminal output of one chat: prompt, streamed model text,
// tool lines, plans, errors and the per-turn status line.
type Renderer struct {
	opts rendererOptions
	live *output.LiveText
	// spin is the waiting line between a model request and its first output.
	spin *thinkingSpinner
	// memory collects this turn's successful memory writes, shown with the
	// status line ("已记住：...").
	memory []string
}

// newRenderer returns a renderer that shows no waiting line yet: the first turn
// starts it (chat prints the banner before that).
func newRenderer(opts rendererOptions) *Renderer {
	r := &Renderer{opts: opts}
	r.spin = newThinkingSpinner(spinnerConfig{
		enabled: opts.Spinner && opts.Color,
		out:     opts.Out,
		paint:   r.paintDim,
		speed:   spinnerSpeed,
		now:     opts.spinnerNow,
		ticks:   opts.spinnerTicks,
	})
	r.resetLive()
	return r
}

// paintDim dims a line (gray, like the tool lines) on a colored terminal.
func (r *Renderer) paintDim(s string) string { return r.paint(color.Gray, s) }

// colorEnabledFor reports whether w is a terminal and NO_COLOR is not set.
func colorEnabledFor(w io.Writer) bool {
	file, ok := w.(*os.File)
	return colorEnabled(ok && term.IsTerminal(int(file.Fd())))
}

// colorEnabled applies the NO_COLOR convention (any non-empty value disables).
func colorEnabled(isTTY bool) bool {
	if v, ok := os.LookupEnv("NO_COLOR"); ok && v != "" {
		return false
	}
	return isTTY
}

func (r *Renderer) paint(c color.Color, s string) string {
	if !r.opts.Color {
		return s
	}
	return c.Text(s)
}

// Prompt returns the input prompt; waiting marks a pending external reply.
func (r *Renderer) Prompt(waiting bool) string {
	if waiting {
		return r.paint(color.Cyan, promptWaitingText)
	}
	return r.paint(color.Cyan, promptText)
}

// BeginTurn resets the per-turn text streaming state and starts the waiting
// line: the model was just asked for a new message.
func (r *Renderer) BeginTurn() {
	r.stopThinking() // nothing of the previous turn may linger on the line
	r.resetLive()
	r.startThinking()
}

// resetLive resets the per-turn text streaming state.
func (r *Renderer) resetLive() {
	r.live = output.NewPrefixedLiveText(r.opts.Out, r.answerPrefix())
	// Markdown needs a terminal: Color is exactly "a terminal without NO_COLOR",
	// so a pipe, a redirected output or NO_COLOR keeps the raw text.
	if r.opts.Markdown && r.opts.Color {
		r.live.SetMarkdown(true)
	}
	r.memory = nil
}

// startThinking shows the waiting line: the model is working on its next
// message. A no-op while the line is already shown or the spinner is off.
func (r *Renderer) startThinking() { r.spin.Start() }

// stopThinking erases that line. Every write of this renderer goes through it
// first, so a frame can never mix into a tool line, an answer or a prompt, and
// the caller prints on a line of its own.
func (r *Renderer) stopThinking() { r.spin.Stop() }

// answerPrefix marks the start of each model message so it stands apart from
// tool lines and the user's input.
func (r *Renderer) answerPrefix() string { return r.paint(color.Green, "▣") + " " }

// Handle renders one runner event: text deltas, tool calls/results and plans.
// Events the chat does not render (run_started, usage, ...) leave the waiting
// line alone; the model may still be answering through them.
func (r *Renderer) Handle(event personal.Event) {
	if stopsWaiting(event) {
		r.stopThinking()
	}
	if r.live.Handle(event) {
		return
	}
	switch event.Kind {
	case "tool_call":
		r.live.Finish() // never start a tool line in the middle of streamed text
		line := "⏺ " + event.ToolName
		if args := truncateLine(event.Text, callArgsLimit); args != "" {
			line += " " + args
		}
		fmt.Fprintln(r.opts.Out, r.paint(color.Gray, line))
	case "tool_result":
		r.live.Finish()
		summary := truncateLine(redact.Secrets(event.Text), resultSummaryLimit)
		if event.IsError {
			fmt.Fprintln(r.opts.Out, r.paint(color.Red, strings.TrimRight("  ⎿ error: "+summary, " ")))
		} else {
			fmt.Fprintln(r.opts.Out, r.paint(color.Gray, strings.TrimRight("  ⎿ ok: "+summary, " ")))
			r.noteMemory(event)
		}
		r.startThinking() // the model reads the result and thinks on
	case "agent_progress":
		// A sub-agent's tool call, under the agent call that started it.
		r.live.Finish()
		line := "  · " + event.ToolName
		if args := truncateLine(event.Text, callArgsLimit); args != "" {
			line += " " + args
		}
		fmt.Fprintln(r.opts.Out, r.paint(color.Gray, line))
		r.startThinking() // the model waits for the sub-agent
	case "plan":
		if event.Plan != nil {
			r.live.Finish()
			r.renderPlan(event)
			r.startThinking()
		}
	case string(agentcore.StreamEventRetry):
		fmt.Fprintln(r.opts.Out, r.paint(color.Yellow, output.RetryNotice(event)))
		r.startThinking() // the request is retried: still waiting for the model
	}
}

// stopsWaiting reports whether event ends the "the model is answering" state:
// its text, a tool call it asked for, a plan it published, or the next request
// after a failed one.
func stopsWaiting(event personal.Event) bool {
	switch event.Kind {
	case string(agentcore.StreamEventTextDelta), string(agentcore.StreamEventMessageStop),
		string(agentcore.StreamEventRetry), "tool_call", "tool_result", "agent_progress":
		return true
	case "plan":
		return event.Plan != nil // an empty plan prints nothing
	}
	return false
}

func (r *Renderer) renderPlan(event personal.Event) {
	head := "⏺ plan"
	if event.Plan.Explanation != "" {
		head += " " + truncateLine(event.Plan.Explanation, callArgsLimit)
	}
	fmt.Fprintln(r.opts.Out, r.paint(color.Gray, head))
	for _, step := range event.Plan.Steps {
		switch step.Status {
		case "completed":
			fmt.Fprintln(r.opts.Out, r.paint(color.Green, fmt.Sprintf("  %c %s", symbols.OK, step.Step)))
		case "in_progress":
			fmt.Fprintln(r.opts.Out, r.paint(color.Yellow, "  ▶ "+step.Step))
		default:
			fmt.Fprintln(r.opts.Out, r.paint(color.Gray, "  ○ "+step.Step))
		}
	}
}

// Finish closes the turn's streamed text and reports whether any was shown.
func (r *Renderer) Finish() bool {
	r.stopThinking()
	return r.live.Finish()
}

// Answer prints a final answer that was not streamed. With markdown on it goes
// through the same renderer as the streamed text (a multi-line answer would
// otherwise show its raw markers).
func (r *Renderer) Answer(text string) {
	r.stopThinking()
	md := r.live.Markdown()
	if md == nil {
		fmt.Fprintln(r.opts.Out, r.answerPrefix()+text)
		return
	}
	fmt.Fprint(r.opts.Out, r.answerPrefix())
	md.Print(text)
}

// External prints input that did not come from this keyboard (a reply sent
// from elsewhere through `suga send` or a Stop hook) in its own color, so it
// stands out from the model's answer. Each line is painted separately.
func (r *Renderer) External(text string) {
	r.stopThinking()
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = r.paint(color.Magenta, line)
	}
	fmt.Fprintln(r.opts.Out, strings.Join(lines, "\n"))
}

// ApprovalDetails formats the full key arguments of an approval question, one
// indented item per detail (continuation lines indented further). Nothing is
// truncated: the person must see exactly what will run.
func (r *Renderer) ApprovalDetails(q tools.Question) string {
	r.stopThinking()
	var b strings.Builder
	for _, detail := range q.Details {
		for i, line := range strings.Split(detail, "\n") {
			indent := "  "
			if i > 0 {
				indent = "    "
			}
			b.WriteString(r.paint(color.Yellow, indent+line))
			b.WriteString("\n")
		}
	}
	return b.String()
}

// ApprovalPrompt is the line the approval answer is typed on.
func (r *Renderer) ApprovalPrompt(q tools.Question) string {
	r.stopThinking()
	return r.paint(color.Yellow, q.Prompt)
}

// Hint prints a gray one-line hint on the chat output.
func (r *Renderer) Hint(text string) {
	r.stopThinking()
	fmt.Fprintln(r.opts.Out, r.paint(color.Gray, text))
}

// Notice prints a plain informational line on the chat output.
func (r *Renderer) Notice(text string) {
	r.stopThinking()
	fmt.Fprintln(r.opts.Out, text)
}

// Error prints a failed turn on the error stream.
func (r *Renderer) Error(err error) {
	r.stopThinking()
	text := "Error: " + err.Error()
	if r.opts.ErrColor {
		text = color.Red.Text(text)
	}
	fmt.Fprintln(r.opts.Err, text)
}

// memoryVerbs names a memory write on the end-of-turn line.
var memoryVerbs = map[string]string{
	native.MemoryActionAdd:     "已记住",
	native.MemoryActionReplace: "已更新",
	native.MemoryActionRemove:  "已删除",
}

// noteMemory remembers a successful memory write of this turn (parsed from
// the memory tool result) for the end-of-turn line.
func (r *Renderer) noteMemory(event personal.Event) {
	if event.ToolName != "memory" {
		return
	}
	change, ok := native.ParseMemoryResult(event.Text)
	if !ok {
		return
	}
	title := truncateLine(redact.Secrets(change.Title), resultSummaryLimit)
	r.memory = append(r.memory, fmt.Sprintf("%s：%s（%s）", memoryVerbs[change.Action], title, change.Scope))
}

// Status prints the grey end-of-turn line: model, token usage and elapsed
// time, preceded by one line per memory write of the turn.
func (r *Renderer) Status(result personal.RunResult, elapsed time.Duration) {
	r.stopThinking()
	for _, line := range r.memory {
		fmt.Fprintln(r.opts.Out, r.paint(color.Green, line))
	}
	r.memory = nil
	var parts []string
	if label := modelLabel(r.opts.Provider, r.opts.Model); label != "" {
		parts = append(parts, label)
	}
	parts = append(parts,
		fmt.Sprintf("in %d / out %d tokens", result.Usage.InputTokens, result.Usage.OutputTokens),
		formatElapsed(elapsed))
	fmt.Fprintln(r.opts.Out, r.paint(color.Gray, strings.Join(parts, " · ")))
}

// Banner prints the chat header: the model in use and the commands.
func (r *Renderer) Banner() {
	r.stopThinking()
	head := "Suga chat"
	if label := modelLabel(r.opts.Provider, r.opts.Model); label != "" {
		head += " · " + label
	}
	if r.opts.Thinking != "" {
		head += " · thinking " + r.opts.Thinking
	}
	if r.opts.ContextWindow > 0 {
		head += " · ctx " + formatTokens(r.opts.ContextWindow)
	}
	fmt.Fprintln(r.opts.Out, r.paint(color.Cyan, head))
	fmt.Fprintln(r.opts.Out, r.paint(color.Gray, "输入任务开始对话；:session 显示会话，:compact 压缩上下文，:quit 退出。"))
	if r.opts.WriteTools {
		fmt.Fprintln(r.opts.Out, r.paint(color.Gray, "工具：可读写文件、执行 shell（tools.approval 决定是否先确认）。"))
	} else {
		// The usual reason the model says it has no shell_exec: write tools
		// are off unless SUGA_HOME/config.yaml sets tools.allow_write.
		fmt.Fprintln(r.opts.Out, r.paint(color.Gray, "工具：只读。要写文件、执行 shell，在 SUGA_HOME/config.yaml 设 tools.allow_write: true。"))
	}
}

// formatTokens renders a token count compactly: 950000 -> "950k",
// 1000000 -> "1M", 1500000 -> "1.5M", 512 -> "512".
func formatTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.2f", float64(n)/1e6), "0"), ".") + "M"
	case n >= 1000:
		return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.1f", float64(n)/1e3), "0"), ".") + "k"
	}
	return strconv.Itoa(n)
}

// ModelLabel is "provider/model" (or whichever part is known).
func ModelLabel(provider, model string) string { return modelLabel(provider, model) }

func modelLabel(provider, model string) string {
	switch {
	case provider != "" && model != "":
		return provider + "/" + model
	case model != "":
		return model
	}
	return provider
}

// FormatElapsed renders a duration for status lines (ms, s, then 1m2s).
func FormatElapsed(d time.Duration) string { return formatElapsed(d) }

func formatElapsed(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		return d.Round(time.Second).String() // e.g. 12m34s, 1h2m3s
	}
}

// truncateLine collapses whitespace (one display line) and cuts to limit runes.
func truncateLine(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit-1]) + "…"
}
