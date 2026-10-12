package native

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/inhere/suag/pkg/agentcore/api"
)

const (
	// minimizeMinBytes: smaller outputs are left as they are.
	minimizeMinBytes = 600
	// minimizeMaxLines caps a tidied output (head and tail kept).
	minimizeMaxLines = 300
	// repeatRun is how many identical consecutive lines are folded into one.
	repeatRun = 3
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// OutputMinimizer tidies successful shell_exec results for the model
// (tools.OutputConfig.Minimize): colour codes and carriage-return progress
// are dropped, runs of identical lines folded, and the output of a command
// an OutputRule matches (go test, cargo test, jest, pytest, maven, ...) is
// reduced to what matters - a summary when everything passed, the failures
// with their messages otherwise (the omp shell minimizer and pi-go's
// compactor do the same). Lines no rule recognises are kept. The output
// layer keeps the original under a ref.
type OutputMinimizer struct {
	rules []OutputRule
}

// NewOutputMinimizer returns a minimizer applying rules (see LoadOutputRules).
func NewOutputMinimizer(rules []OutputRule) *OutputMinimizer {
	return &OutputMinimizer{rules: rules}
}

var defaultMinimizer = sync.OnceValue(func() *OutputMinimizer {
	return NewOutputMinimizer(DefaultOutputRules())
})

// MinimizeToolOutput tidies with the built-in rules only.
func MinimizeToolOutput(call api.ToolCall, content string) (string, bool) {
	return defaultMinimizer().Minimize(call, content)
}

// Minimize returns the tidied content; ok is false when nothing worth it
// changed.
func (m *OutputMinimizer) Minimize(call api.ToolCall, content string) (string, bool) {
	if call.Name != "shell_exec" || len(content) < minimizeMinBytes {
		return "", false
	}
	command, _ := call.Arguments["command"].(string)
	lines := strings.Split(cleanTerminal(content), "\n")
	for _, r := range m.rules {
		if r.compiled != nil && r.compiled.match.MatchString(command) {
			lines = r.compiled.apply(lines, r.Summary)
		}
	}
	lines = foldRepeats(lines)
	if len(lines) > minimizeMaxLines {
		head, tail := minimizeMaxLines*2/3, minimizeMaxLines/3
		cut := len(lines) - head - tail
		lines = append(append(lines[:head:head], fmt.Sprintf("[... %d lines omitted ...]", cut)), lines[len(lines)-tail:]...)
	}
	tidy := strings.Join(lines, "\n")
	// Worth it only when it saves a fair share.
	if len(content)-len(tidy) < len(content)/5 {
		return "", false
	}
	return tidy, true
}

// cleanTerminal removes ANSI escapes and keeps, for a line redrawn with \r
// (progress bars), only its last state.
func cleanTerminal(s string) string {
	s = ansiPattern.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.Contains(s, "\r") {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if j := strings.LastIndex(strings.TrimRight(l, "\r"), "\r"); j >= 0 {
			lines[i] = l[j+1:]
		}
	}
	return strings.Join(lines, "\n")
}

// foldRepeats folds runs of repeatRun or more identical lines into one line
// and a count.
func foldRepeats(lines []string) []string {
	var out []string
	for i := 0; i < len(lines); {
		j := i + 1
		for j < len(lines) && lines[j] == lines[i] {
			j++
		}
		out = append(out, lines[i])
		if n := j - i; n >= repeatRun && strings.TrimSpace(lines[i]) != "" {
			out = append(out, fmt.Sprintf("[previous line repeated %d more times]", n-1))
		} else {
			for k := i + 1; k < j; k++ {
				out = append(out, lines[k])
			}
		}
		i = j
	}
	return out
}
