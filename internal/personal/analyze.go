package personal

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/inhere/suag/internal/personal/store"
)

// testCommandPattern recognises shell commands that run tests.
var testCommandPattern = regexp.MustCompile(`\b(go test|pytest|cargo test|npm (run )?test|pnpm (run )?test|yarn test|jest|vitest|mvn( \S+)* test|gradle( \S+)* test|make test|phpunit|rspec|ctest|dotnet test)\b`)

// TraceAnalysis summarizes a session's events for loop analysis (suag trace
// --analyze): how many steps, which tools, repeated reads and commands, and
// how much verification happened after the tests first passed. It is derived
// from the events alone (tool attrs arg/target/exit_code, model attr
// tool_calls), so older sessions without these attrs give partial numbers.
type TraceAnalysis struct {
	ModelCalls      int            `json:"model_calls"`
	ToolCalls       int            `json:"tool_calls"`
	ParallelSteps   int            `json:"parallel_steps"`     // model calls that issued more than one tool call
	MaxCallsPerStep int            `json:"max_calls_per_step"` // most tool calls one model call issued
	InputTokens     int64          `json:"input_tokens"`
	CacheReadTokens int64          `json:"cache_read_tokens"`
	OutputTokens    int64          `json:"output_tokens"`
	ReasoningChars  int            `json:"reasoning_chars"`
	TextChars       int            `json:"text_chars"`
	Tools           map[string]int `json:"tools"`
	ToolErrors      map[string]int `json:"tool_errors,omitempty"`
	// Rereads lists files read more than once; ReadsAfterWrite counts reads
	// of a file this session had already written or edited.
	Rereads         []Count `json:"rereads,omitempty"`
	ReadsAfterWrite int     `json:"reads_after_write"`
	EditsPerFile    []Count `json:"edits_per_file,omitempty"`
	// Shell: runs, failures (exit code != 0) and identical commands run again.
	ShellRuns        int     `json:"shell_runs"`
	ShellFailures    int     `json:"shell_failures"`
	RepeatedCommands []Count `json:"repeated_commands,omitempty"`
	// Tests: shell runs that look like test commands, and how many of them
	// came after the first passing one (re-verification).
	TestRuns          int `json:"test_runs"`
	TestRunsAfterPass int `json:"test_runs_after_pass"`
	Spills            int `json:"spills"`
	Folds             int `json:"folds"`
	Compactions       int `json:"compactions"`
	// Slowest are the longest model calls.
	Slowest []SlowCall `json:"slowest,omitempty"`
}

// Count is a named count (a path or a command).
type Count struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// SlowCall is one long model call: its step number, duration and output.
type SlowCall struct {
	Step         int   `json:"step"`
	DurationMs   int64 `json:"duration_ms"`
	OutputTokens int64 `json:"output_tokens"`
	ToolCalls    int   `json:"tool_calls"`
}

// AnalyzeTrace derives a TraceAnalysis from a session's events (in order).
func AnalyzeTrace(events []store.Event) TraceAnalysis {
	a := TraceAnalysis{Tools: map[string]int{}, ToolErrors: map[string]int{}}
	reads, edits, commands := map[string]int{}, map[string]int{}, map[string]int{}
	written := map[string]bool{}
	passed := false
	var slow []SlowCall
	for _, e := range events {
		switch e.Kind {
		case store.EventModel:
			if p, _ := e.Attrs["purpose"].(string); p != "" {
				continue // compaction and other side calls are not steps
			}
			a.ModelCalls++
			calls := attrInt(e.Attrs["tool_calls"])
			if calls > 1 {
				a.ParallelSteps++
			}
			a.MaxCallsPerStep = max(a.MaxCallsPerStep, calls)
			a.InputTokens += e.InputTokens
			a.CacheReadTokens += e.CacheReadTokens
			a.OutputTokens += e.OutputTokens
			a.ReasoningChars += attrInt(e.Attrs["reasoning_chars"])
			a.TextChars += attrInt(e.Attrs["text_chars"])
			slow = append(slow, SlowCall{Step: a.ModelCalls, DurationMs: e.DurationMs, OutputTokens: e.OutputTokens, ToolCalls: calls})
		case store.EventTool, store.EventDenial:
			a.ToolCalls++
			a.Tools[e.Name]++
			if e.Status != eventOK {
				a.ToolErrors[e.Name]++
			}
			if spilled, _ := e.Attrs["spilled"].(bool); spilled {
				a.Spills++
			}
			targets := attrStrings(e.Attrs["target"])
			switch e.Name {
			case "file_read":
				for _, t := range targets {
					reads[t]++
					if written[t] {
						a.ReadsAfterWrite++
					}
				}
			case "file_write", "file_edit":
				for _, t := range targets {
					edits[t]++
					written[t] = true
				}
			case "shell_exec":
				a.ShellRuns++
				arg, _ := e.Attrs["arg"].(string)
				commands[arg]++
				code, hasCode := e.Attrs["exit_code"]
				failed := e.Status != eventOK || (hasCode && attrInt(code) != 0)
				if failed {
					a.ShellFailures++
				}
				if testCommandPattern.MatchString(arg) {
					a.TestRuns++
					if passed {
						a.TestRunsAfterPass++
					}
					if !failed && hasCode {
						passed = true
					}
				}
			}
		case store.EventFold:
			a.Folds++
		case store.EventCompaction:
			if e.Name == "compact" {
				a.Compactions++
			}
		}
	}
	a.Rereads = countsAbove(reads, 1)
	a.EditsPerFile = countsAbove(edits, 0)
	a.RepeatedCommands = countsAbove(commands, 1)
	sort.SliceStable(slow, func(i, j int) bool { return slow[i].DurationMs > slow[j].DurationMs })
	a.Slowest = slow[:min(len(slow), 3)]
	if len(a.ToolErrors) == 0 {
		a.ToolErrors = nil
	}
	return a
}

// WriteTraceAnalysis prints the analysis as text, or as one JSON object.
func WriteTraceAnalysis(w io.Writer, a TraceAnalysis, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(w).Encode(a)
	}
	var b strings.Builder
	row := func(label, format string, args ...any) {
		fmt.Fprintf(&b, "%-22s %s\n", label, fmt.Sprintf(format, args...))
	}
	row("steps", "%d model calls, %d tool calls, %d parallel steps (max %d calls in one step)",
		a.ModelCalls, a.ToolCalls, a.ParallelSteps, a.MaxCallsPerStep)
	row("tokens", "in %s (cache read %s), out %s; reasoning %s chars, text %s chars",
		human(a.InputTokens), human(a.CacheReadTokens), human(a.OutputTokens), human(int64(a.ReasoningChars)), human(int64(a.TextChars)))
	row("tools", "%s", joinCounts(sortedCounts(a.Tools), a.ToolErrors))
	row("re-reads", "%s", orNone(joinCounts(a.Rereads, nil)))
	row("reads after own write", "%d", a.ReadsAfterWrite)
	row("edits per file", "%s", orNone(joinCounts(a.EditsPerFile, nil)))
	row("shell", "%d runs, %d failed (exit != 0)", a.ShellRuns, a.ShellFailures)
	row("repeated commands", "%s", orNone(joinCounts(a.RepeatedCommands, nil)))
	row("tests", "%d runs, %d after the first pass", a.TestRuns, a.TestRunsAfterPass)
	row("spills / folds", "%d / %d (compactions %d)", a.Spills, a.Folds, a.Compactions)
	var slow []string
	for _, s := range a.Slowest {
		slow = append(slow, fmt.Sprintf("step %d %.1fs out=%d calls=%d", s.Step, float64(s.DurationMs)/1000, s.OutputTokens, s.ToolCalls))
	}
	row("slowest model calls", "%s", orNone(strings.Join(slow, "; ")))
	_, err := io.WriteString(w, b.String())
	return err
}

func attrInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}

func attrStrings(v any) []string {
	switch list := v.(type) {
	case []string:
		return list
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// countsAbove returns the entries with a count above min, most first.
func countsAbove(m map[string]int, min int) []Count {
	var out []Count
	for name, n := range m {
		if n > min && name != "" {
			out = append(out, Count{Name: name, Count: n})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func sortedCounts(m map[string]int) []Count { return countsAbove(m, 0) }

// joinCounts renders "name xN" items (the first 8), with an error count from
// errs when present.
func joinCounts(counts []Count, errs map[string]int) string {
	var parts []string
	for i, c := range counts {
		if i == 8 {
			parts = append(parts, fmt.Sprintf("... %d more", len(counts)-8))
			break
		}
		item := fmt.Sprintf("%s x%d", c.Name, c.Count)
		if n := errs[c.Name]; n > 0 {
			item += fmt.Sprintf(" (%d failed)", n)
		}
		parts = append(parts, item)
	}
	return strings.Join(parts, ", ")
}

func orNone(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// human renders a count compactly: 1380000 -> "1.38M", 60100 -> "60.1k".
func human(n int64) string {
	switch {
	case n >= 1_000_000:
		return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.2f", float64(n)/1e6), "0"), ".") + "M"
	case n >= 1000:
		return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.1f", float64(n)/1e3), "0"), ".") + "k"
	}
	return fmt.Sprint(n)
}
