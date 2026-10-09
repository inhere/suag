package chat

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gookit/cliui/show/symbols"
	"github.com/gookit/color"
	"github.com/inhere/suag/internal/output"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/internal/platform/tools"
	"golang.org/x/term"
)

const (
	// resultSummaryLimit caps the one-line tool result summary, in runes.
	resultSummaryLimit = 100
	// callArgsLimit caps the tool_call argument text on screen, in runes.
	callArgsLimit = 120

	promptText        = "> "
	promptWaitingText = "> (可等待外部回复) "
)

// rendererOptions configures a Renderer. Color switches ANSI styling for Out
// and ErrColor for Err; both false yields plain text (symbols stay).
type rendererOptions struct {
	Out, Err        io.Writer
	Color, ErrColor bool
	// Provider and Model label the per-turn status line; either may be empty.
	Provider, Model string
}

// Renderer owns all terminal output of one chat: prompt, streamed model text,
// tool lines, plans, errors and the per-turn status line.
type Renderer struct {
	opts rendererOptions
	live *output.LiveText
}

func newRenderer(opts rendererOptions) *Renderer {
	return &Renderer{opts: opts, live: output.NewLiveText(opts.Out)}
}

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

// BeginTurn resets the per-turn text streaming state.
func (r *Renderer) BeginTurn() { r.live = output.NewLiveText(r.opts.Out) }

// Handle renders one runner event: text deltas, tool calls/results and plans.
func (r *Renderer) Handle(event personal.Event) {
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
		summary := truncateLine(personal.RedactSecretValues(event.Text), resultSummaryLimit)
		if event.IsError {
			fmt.Fprintln(r.opts.Out, r.paint(color.Red, strings.TrimRight("  ⎿ error: "+summary, " ")))
		} else {
			fmt.Fprintln(r.opts.Out, r.paint(color.Gray, strings.TrimRight("  ⎿ ok: "+summary, " ")))
		}
	case "plan":
		if event.Plan != nil {
			r.live.Finish()
			r.renderPlan(event)
		}
	}
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
func (r *Renderer) Finish() bool { return r.live.Finish() }

// Answer prints a final answer that was not streamed.
func (r *Renderer) Answer(text string) { fmt.Fprintln(r.opts.Out, text) }

// External prints input that did not come from this keyboard (a reply sent
// from elsewhere through `suag send` or a Stop hook) in its own color, so it
// stands out from the model's answer. Each line is painted separately.
func (r *Renderer) External(text string) {
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
	return r.paint(color.Yellow, q.Prompt)
}

// Notice prints a plain informational line on the chat output.
func (r *Renderer) Notice(text string) { fmt.Fprintln(r.opts.Out, text) }

// Error prints a failed turn on the error stream.
func (r *Renderer) Error(err error) {
	text := "Error: " + err.Error()
	if r.opts.ErrColor {
		text = color.Red.Text(text)
	}
	fmt.Fprintln(r.opts.Err, text)
}

// Status prints the grey end-of-turn line: model, token usage and elapsed time.
func (r *Renderer) Status(result personal.RunResult, elapsed time.Duration) {
	var parts []string
	if label := modelLabel(r.opts.Provider, r.opts.Model); label != "" {
		parts = append(parts, label)
	}
	parts = append(parts,
		fmt.Sprintf("in %d / out %d tokens", result.Usage.InputTokens, result.Usage.OutputTokens),
		formatElapsed(elapsed))
	fmt.Fprintln(r.opts.Out, r.paint(color.Gray, strings.Join(parts, " · ")))
}

func modelLabel(provider, model string) string {
	switch {
	case provider != "" && model != "":
		return provider + "/" + model
	case model != "":
		return model
	}
	return provider
}

func formatElapsed(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
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
