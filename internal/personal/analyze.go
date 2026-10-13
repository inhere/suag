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

// focusedTestPattern recognises test runs limited to some tests.
var focusedTestPattern = regexp.MustCompile(`(^|\s)(-run|-k|--test|-t)(\s|=)`)

// TraceAnalysis summarizes a session's events for loop analysis (suag trace
// --analyze): how many steps, which tools, repeated reads and commands, and
// how much verification happened after the tests first passed. It is derived
// from the events alone (tool attrs arg/target/exit_code, model attr
// tool_calls), so older sessions without these attrs give partial numbers.
type TraceAnalysis struct {
	ModelCalls      int   `json:"model_calls"`
	ToolCalls       int   `json:"tool_calls"`
	ParallelSteps   int   `json:"parallel_steps"`     // model calls that issued more than one tool call
	MaxCallsPerStep int   `json:"max_calls_per_step"` // most tool calls one model call issued
	InputTokens     int64 `json:"input_tokens"`
	CacheReadTokens int64 `json:"cache_read_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	// ModelMs and ToolMs are the time spent in main model calls and in tool
	// calls (side calls such as compaction are not counted).
	ModelMs        int64          `json:"model_ms"`
	ToolMs         int64          `json:"tool_ms"`
	ReasoningChars int            `json:"reasoning_chars"`
	TextChars      int            `json:"text_chars"`
	Tools          map[string]int `json:"tools"`
	ToolErrors     map[string]int `json:"tool_errors,omitempty"`
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
	// Phases, in model calls: before the first write or edit, from that step
	// to the first passing test run, and after it (-1 when the run never got
	// there). A test run passes when its output reports no failure (attr
	// test_failed) and it exited 0; a focused run (-run, -k) does not count.
	ExploreSteps      int `json:"explore_steps"`
	WriteToGreenSteps int `json:"write_to_green_steps"`
	AfterGreenSteps   int `json:"after_green_steps"`
	// EditSteps are model calls that wrote or edited files; SingleEditSteps
	// those whose only call made a single change (one file_edit replacement
	// or one file_write): fixes sent one per step instead of together.
	EditSteps       int `json:"edit_steps"`
	SingleEditSteps int `json:"single_edit_steps"`
	// ShellStderrOK counts shell runs that exited 0 but wrote to stderr (a
	// failing command before ";" hides its exit code).
	ShellStderrOK int `json:"shell_stderr_exit0"`
	Spills        int `json:"spills"`
	Minimized     int `json:"minimized"` // results tidied by the output minimizer
	Folds         int `json:"folds"`
	// Cache attribution from the model attrs prefix*: the input tokens the
	// provider did not serve from its cache, split into content new since
	// the previous request, earlier content sent again after a prefix break
	// (by reason: fold, segment, system, tools, rewrite) and content the
	// prefix should have let the provider serve but it did not (expired or
	// not yet written cache). Estimated from JSON sizes; calls without the
	// attrs (older sessions) count in Uncached only.
	Uncached         int64            `json:"uncached_input_tokens"`
	UncachedFresh    int64            `json:"uncached_fresh"`
	UncachedRebuilt  map[string]int64 `json:"uncached_rebuilt,omitempty"`
	UncachedProvider int64            `json:"uncached_provider"`
	PrefixBreaks     map[string]int   `json:"prefix_breaks,omitempty"`
	Compactions      int              `json:"compactions"`
	// Retries are the model request retries after transient failures (every
	// model call, side calls included) and RetryWaitMs the time waited.
	Retries     int   `json:"retries"`
	RetryWaitMs int64 `json:"retry_wait_ms"`
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
	a := TraceAnalysis{Tools: map[string]int{}, ToolErrors: map[string]int{},
		UncachedRebuilt: map[string]int64{}, PrefixBreaks: map[string]int{}}
	reads, edits, commands := map[string]int{}, map[string]int{}, map[string]int{}
	written := map[string]bool{}
	passed := false
	var slow []SlowCall
	firstWrite, green := -1, -1
	// wroteLater: the run writes or edits a file at some point; a pass
	// before that write is then no "first pass" (see the test runs below).
	wroteLater := false
	for _, e := range events {
		if e.Kind == store.EventTool && (e.Name == "file_write" || e.Name == "file_edit") {
			wroteLater = true
			break
		}
	}
	// the current step's calls: how many, how many edits, changes made
	stepCalls, stepEdits, stepChanges := 0, 0, 0
	closeStep := func() {
		if stepEdits > 0 {
			a.EditSteps++
			if stepCalls == 1 && stepChanges == 1 {
				a.SingleEditSteps++
			}
		}
		stepCalls, stepEdits, stepChanges = 0, 0, 0
	}
	for _, e := range events {
		switch e.Kind {
		case store.EventModel:
			a.Retries += attrInt(e.Attrs["retries"])
			a.RetryWaitMs += int64(attrInt(e.Attrs["retry_wait_ms"]))
			if p, _ := e.Attrs["purpose"].(string); p != "" {
				continue // compaction and other side calls are not steps
			}
			closeStep()
			a.ModelCalls++
			calls := attrInt(e.Attrs["tool_calls"])
			if calls > 1 {
				a.ParallelSteps++
			}
			a.MaxCallsPerStep = max(a.MaxCallsPerStep, calls)
			a.ModelMs += e.DurationMs
			a.InputTokens += e.InputTokens
			a.CacheReadTokens += e.CacheReadTokens
			a.OutputTokens += e.OutputTokens
			a.attributeCache(e)
			a.ReasoningChars += attrInt(e.Attrs["reasoning_chars"])
			a.TextChars += attrInt(e.Attrs["text_chars"])
			slow = append(slow, SlowCall{Step: a.ModelCalls, DurationMs: e.DurationMs, OutputTokens: e.OutputTokens, ToolCalls: calls})
		case store.EventTool, store.EventDenial:
			a.ToolCalls++
			a.ToolMs += e.DurationMs
			stepCalls++
			a.Tools[e.Name]++
			if e.Status != eventOK {
				a.ToolErrors[e.Name]++
			}
			if spilled, _ := e.Attrs["spilled"].(bool); spilled {
				a.Spills++
			}
			if minimized, _ := e.Attrs["minimized"].(bool); minimized {
				a.Minimized++
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
				if firstWrite < 0 {
					firstWrite = a.ModelCalls
				}
				stepEdits++
				stepChanges += max(attrInt(e.Attrs["changes"]), 1)
			case "shell_exec":
				a.ShellRuns++
				arg, _ := e.Attrs["arg"].(string)
				commands[arg]++
				code, hasCode := e.Attrs["exit_code"]
				failed := e.Status != eventOK || (hasCode && attrInt(code) != 0)
				if failed {
					a.ShellFailures++
				} else if stderr, _ := e.Attrs["stderr"].(bool); stderr {
					a.ShellStderrOK++
				}
				testFailed, reported := e.Attrs["test_failed"].(bool)
				// A focused run (go test -run X, pytest -k X) checks one test,
				// often a scratch one written to probe a bug: it is no "green".
				focused := focusedTestPattern.MatchString(arg)
				if reported || testCommandPattern.MatchString(arg) {
					a.TestRuns++
					// After the first pass that followed a change: a test run
					// before any write (a baseline check) does not count.
					if passed {
						a.TestRunsAfterPass++
					}
					if !failed && hasCode && !testFailed && !focused && (firstWrite >= 0 || !wroteLater) {
						passed = true
						if green < 0 && firstWrite >= 0 {
							green = a.ModelCalls
						}
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
	closeStep()
	// Steps are 1-based model calls; firstWrite and green name the step a
	// call belonged to.
	a.ExploreSteps, a.WriteToGreenSteps, a.AfterGreenSteps = a.ModelCalls, -1, -1
	if firstWrite > 0 {
		a.ExploreSteps = firstWrite - 1
	}
	if green > 0 {
		a.WriteToGreenSteps = green - firstWrite + 1
		a.AfterGreenSteps = a.ModelCalls - green
	}
	a.Rereads = countsAbove(reads, 1)
	a.EditsPerFile = countsAbove(edits, 0)
	a.RepeatedCommands = countsAbove(commands, 1)
	sort.SliceStable(slow, func(i, j int) bool { return slow[i].DurationMs > slow[j].DurationMs })
	a.Slowest = slow[:min(len(slow), 3)]
	if len(a.ToolErrors) == 0 {
		a.ToolErrors = nil
	}
	if len(a.UncachedRebuilt) == 0 {
		a.UncachedRebuilt = nil
	}
	if len(a.PrefixBreaks) == 0 {
		a.PrefixBreaks = nil
	}
	return a
}

// attributeCache splits one model call's uncached input by cause (see
// TraceAnalysis.Uncached*): the reported input tokens are spread over the
// request's reused, rebuilt and fresh parts by their JSON sizes; what the
// provider left uncached beyond rebuilt + fresh is a provider-side miss, and
// when it served more than expected the shares shrink in proportion.
func (a *TraceAnalysis) attributeCache(e store.Event) {
	miss := max(e.InputTokens-e.CacheReadTokens, 0)
	a.Uncached += miss
	reason, _ := e.Attrs["prefix"].(string)
	reused, rebuilt, fresh := attrInt(e.Attrs["prefix_reused"]), attrInt(e.Attrs["prefix_rebuilt"]), attrInt(e.Attrs["prefix_fresh"])
	total := reused + rebuilt + fresh
	if reason == "" || total == 0 {
		return
	}
	if reason != prefixAppend && reason != prefixFirst {
		a.PrefixBreaks[reason]++
	}
	perChar := float64(e.InputTokens) / float64(total)
	expFresh, expRebuilt := float64(fresh)*perChar, float64(rebuilt)*perChar
	if expected := expFresh + expRebuilt; expected > float64(miss) && expected > 0 {
		scale := float64(miss) / expected
		expFresh, expRebuilt = expFresh*scale, expRebuilt*scale
	}
	a.UncachedFresh += int64(expFresh)
	if rebuilt > 0 {
		a.UncachedRebuilt[reason] += int64(expRebuilt)
	}
	a.UncachedProvider += max(miss-int64(expFresh)-int64(expRebuilt), 0)
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
	row("time", "model %.1fs, tools %.1fs", float64(a.ModelMs)/1000, float64(a.ToolMs)/1000)
	row("tools", "%s", joinCounts(sortedCounts(a.Tools), a.ToolErrors))
	row("re-reads", "%s", orNone(joinCounts(a.Rereads, nil)))
	row("reads after own write", "%d", a.ReadsAfterWrite)
	row("edits per file", "%s", orNone(joinCounts(a.EditsPerFile, nil)))
	row("shell", "%d runs, %d failed (exit != 0), %d exited 0 with stderr output", a.ShellRuns, a.ShellFailures, a.ShellStderrOK)
	row("repeated commands", "%s", orNone(joinCounts(a.RepeatedCommands, nil)))
	row("tests", "%d runs, %d after the first pass", a.TestRuns, a.TestRunsAfterPass)
	row("phases", "explore %d, first write to green %s, after green %s (model calls)",
		a.ExploreSteps, phaseSteps(a.WriteToGreenSteps), phaseSteps(a.AfterGreenSteps))
	row("edit steps", "%d, %d with a single change (one file_edit replacement or one file_write)", a.EditSteps, a.SingleEditSteps)
	row("uncached input", "%s", uncachedSummary(a))
	row("spills / folds", "%d / %d (compactions %d, tidied outputs %d)", a.Spills, a.Folds, a.Compactions, a.Minimized)
	row("model retries", "%d (waited %.1fs)", a.Retries, float64(a.RetryWaitMs)/1000)
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

// uncachedSummary renders the cache attribution row: the total, then the
// estimated parts and the prefix breaks by reason.
func uncachedSummary(a TraceAnalysis) string {
	out := human(a.Uncached)
	if a.UncachedFresh == 0 && len(a.UncachedRebuilt) == 0 && a.UncachedProvider == 0 {
		return out // no prefix attrs (older session)
	}
	var rebuilt []string
	var sum int64
	for _, c := range sortedCounts(intCounts(a.UncachedRebuilt)) {
		rebuilt = append(rebuilt, fmt.Sprintf("%s %s x%d", c.Name, human(a.UncachedRebuilt[c.Name]), a.PrefixBreaks[c.Name]))
		sum += a.UncachedRebuilt[c.Name]
	}
	out += fmt.Sprintf(" ≈ fresh %s + rebuilt %s", human(a.UncachedFresh), human(sum))
	if len(rebuilt) > 0 {
		out += " (" + strings.Join(rebuilt, ", ") + ")"
	}
	return out + fmt.Sprintf(" + provider-side %s", human(a.UncachedProvider))
}

// intCounts narrows token sums to ints for sorting by size.
func intCounts(m map[string]int64) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = int(v)
	}
	return out
}

// phaseSteps renders a phase length, "-" when the run never got there.
func phaseSteps(n int) string {
	if n < 0 {
		return "-"
	}
	return fmt.Sprint(n)
}
