package native

import (
	"fmt"
	"regexp"
	"strings"

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

var (
	ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	// Verbose go test lines that carry no information once a test passed.
	goTestNoise = regexp.MustCompile(`^\s*(=== (RUN|PAUSE|CONT|NAME)\s|--- (PASS|SKIP): |PASS$)`)
	// A failure: a failed test or package, a panic, or a build error line.
	goTestFail   = regexp.MustCompile(`^\s*--- FAIL: |^FAIL\b|^panic: |^\S+\.go:\d+:\d+: `)
	goTestPassed = regexp.MustCompile(`^\s*--- PASS: `)
	goPkgOK      = regexp.MustCompile(`^ok\s+\S+`)
)

// MinimizeToolOutput tidies a successful shell_exec result for the model
// (tools.OutputConfig.Minimize): colour codes and carriage-return progress
// are dropped, runs of identical lines folded, and go test output reduced to
// what matters - one line per package when everything passed, the failures
// with their messages otherwise (the omp shell minimizer and pi-go's
// compactor do the same). Lines it does not recognise are kept. The output
// layer keeps the original under a ref. ok is false when nothing worth it
// changed.
func MinimizeToolOutput(call api.ToolCall, content string) (string, bool) {
	if call.Name != "shell_exec" || len(content) < minimizeMinBytes {
		return "", false
	}
	command, _ := call.Arguments["command"].(string)
	lines := strings.Split(cleanTerminal(content), "\n")
	if strings.Contains(command, "go test") {
		lines = tidyGoTest(lines)
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

// tidyGoTest drops the verbose lines of passing tests (=== RUN, --- PASS,
// the log lines indented around them, a lone PASS) and keeps everything
// else: failures with their messages and logs, package ok/FAIL lines, build
// errors, other commands' output. When no failure is left it appends a
// one-line summary.
func tidyGoTest(lines []string) []string {
	var out, pending []string // pending: indented lines of the running test
	passed, pkgs, failed := 0, 0, false
	inPassed := false // inside the indented log printed after "--- PASS"
	flush := func() {
		out = append(out, pending...)
		pending = pending[:0]
	}
	for _, l := range lines {
		indented := strings.HasPrefix(l, "    ")
		switch {
		case goTestPassed.MatchString(l):
			passed++
			pending, inPassed = pending[:0], true // the passed test's own log goes too
			continue
		case indented && inPassed:
			continue
		case indented && !goTestFail.MatchString(l):
			pending = append(pending, l) // decided by the test's result line
			continue
		}
		inPassed = false
		if goTestNoise.MatchString(l) {
			if strings.Contains(l, "=== RUN") {
				flush()
			}
			continue
		}
		flush()
		if goPkgOK.MatchString(l) {
			pkgs++
		}
		if goTestFail.MatchString(l) {
			failed = true
		}
		out = append(out, l)
	}
	flush()
	if !failed && pkgs > 0 {
		summary := fmt.Sprintf("go test: %d package(s) ok", pkgs)
		if passed > 0 {
			summary += fmt.Sprintf(", %d test(s) passed", passed)
		}
		out = append(out, summary)
	}
	return out
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
