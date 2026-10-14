package native

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// newTestRule compiles one rule written inline. The tests of the rule steps
// lower the gates to 0 (min_bytes/min_saving) so every input reaches the step
// under test, and the step itself is usually called directly on lines.
func newTestRule(t *testing.T, rule string) *OutputRule {
	t.Helper()
	rules, errs := parseOutputRules([]byte("rules:\n  - name: fake\n    match: '^fake\\b'\n" + rule))
	assert.Empty(t, errs)
	assert.Len(t, rules, 1)
	return &rules[0]
}

// newTestMinimizer is newTestRule with a minimizer applying that rule.
func newTestMinimizer(t *testing.T, rule string) (*OutputMinimizer, *OutputRule) {
	t.Helper()
	r := newTestRule(t, rule)
	return NewOutputMinimizer([]OutputRule{*r}), r
}

// gatesOff is the rule body the step tests use: with both gates at 0 every
// input reaches the step under test.
const gatesOff = "    min_bytes: 0\n    min_saving: 0\n"

// TestOutputRuleReplace covers the `replace` items: they rewrite what the rule
// kept, after drop/fold/block, in the order they are written.
func TestOutputRuleReplace(t *testing.T) {
	cases := []struct {
		name, replace, input string
		want                 []string
	}{
		{
			name:    "a capture group by number",
			replace: gatesOff + "    replace:\n      - pattern: '^(\\w+): (\\d+) items$'\n        with: '$1 has $2'\n",
			input:   "apples: 41 items\n[exit code: 0]",
			want:    []string{"apples has 41", "[exit code: 0]"},
		},
		{
			name:    "a named group, and the items run in order",
			replace: gatesOff + "    replace:\n      - pattern: '^(?P<count>\\d+) file\\(s\\)$'\n        with: '${count} files changed'\n      - pattern: 'changed$'\n        with: 'touched'\n",
			input:   "3 file(s)\n[exit code: 0]",
			want:    []string{"3 files touched", "[exit code: 0]"},
		},
		{
			name:    "a pattern spans lines",
			replace: gatesOff + "    replace:\n      - pattern: '^(up|down)\\n(from )?(\\S+)$'\n        with: '$1: $3'\n",
			input:   "up\nfrom build/cache\ndone\n[exit code: 0]",
			want:    []string{"up: build/cache", "done", "[exit code: 0]"},
		},
		{
			name:    "every occurrence on a line is replaced",
			replace: gatesOff + "    replace:\n      - pattern: '(\\d+)s$'\n        with: '$1 sec'\n",
			input:   "step 1 took 3s\nstep 2 took 12s\n[exit code: 0]",
			want:    []string{"step 1 took 3 sec", "step 2 took 12 sec", "[exit code: 0]"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The step itself, so the test reads what replace produced: the
			// minimizer would only hand it back when it is shorter.
			rule := newTestRule(t, tc.replace)
			assert.Eq(t, tc.want, rule.compiled.applyReplace(strings.Split(tc.input, "\n")))
		})
	}

	t.Run("an empty with only drops what matched", func(t *testing.T) {
		rule := newTestRule(t, gatesOff+"    replace:\n      - pattern: '^debug: '\n        with: ''\n")
		assert.Eq(t, []string{"kept", "noise", "debug"}, rule.compiled.applyReplace([]string{"kept", "debug: noise", "debug"}))
	})

	t.Run("replace runs after drop and before the summary", func(t *testing.T) {
		m, _ := newTestMinimizer(t, gatesOff+
			"    drop: ['^noise']\n    count: {rows: '^row'}\n    summary: 'kept {rows} row(s)'\n"+
			"    replace:\n      - pattern: '^row of (\\w+) output$'\n        with: '$1'\n")
		tidy, ok := m.Minimize(shellCall("fake tool"), strings.Repeat("row of ok output\n", 20)+"noise line\n[exit code: 0]")
		assert.Require(t, assert.True(t, ok))
		// The summary is appended after replace (it keeps its own wording), and
		// the generic repeat folding still runs over the rule's result.
		assert.Eq(t, "ok\n[previous line repeated 19 more times]\n[exit code: 0]\nkept 20 row(s)", tidy)
	})
}

// TestOutputRuleGroupBy covers `group_by`: neighbouring lines sharing the
// captured key merge into one header line and the rest of each line, indented.
func TestOutputRuleGroupBy(t *testing.T) {
	t.Run("the first capture group is the key", func(t *testing.T) {
		rule := newTestRule(t, gatesOff+"    group_by: '^([^:]+):'\n")
		got := rule.compiled.groupBy([]string{"a.go:12:x", "a.go:13:y", "b.go:1:z"})
		// b.go has a single line: grouping it would only add a line, so it keeps
		// its own shape.
		assert.Eq(t, []string{"a.go:", "  12:x", "  13:y", "b.go:1:z"}, got)
	})

	t.Run("a named group wins over the first one", func(t *testing.T) {
		rule := newTestRule(t, gatesOff+"    group_by: '^(\\w+):(?P<file>[^:]+):'\n")
		got := rule.compiled.groupBy([]string{"A:a.go:1:x", "B:a.go:2:y", "A:b.go:3:z"})
		// The keys are the file names, so the two a.go lines (different prefixes)
		// are one group: the header is what matched on its first line, the rest of
		// every line follows it indented.
		assert.Eq(t, []string{"A:a.go:", "  1:x", "  2:y", "A:b.go:3:z"}, got)
	})

	t.Run("only neighbours are merged", func(t *testing.T) {
		rule := newTestRule(t, gatesOff+"    group_by: '^([^:]+):'\n")
		got := rule.compiled.groupBy([]string{"a.go:1:x", "a.go:2:y", "no match here", "a.go:3:z"})
		assert.Eq(t, []string{"a.go:", "  1:x", "  2:y", "no match here", "a.go:3:z"}, got)
	})

	t.Run("a windows path keeps its drive letter", func(t *testing.T) {
		rule := newTestRule(t, gatesOff+"    group_by: '^((?:[A-Za-z]:)?[^:]+):'\n")
		got := rule.compiled.groupBy([]string{`D:\work\a.go:12:x`, `D:\work\a.go:13:y`})
		assert.Eq(t, []string{`D:\work\a.go:`, "  12:x", "  13:y"}, got)
	})

	t.Run("a line that is only the key repeats it as its body", func(t *testing.T) {
		rule := newTestRule(t, gatesOff+"    group_by: '^(build/\\S+)$'\n")
		got := rule.compiled.groupBy([]string{"build/x", "build/x"})
		assert.Eq(t, []string{"build/x", "  build/x", "  build/x"}, got)
	})

	t.Run("without group_by the lines are left alone", func(t *testing.T) {
		rule := newTestRule(t, gatesOff)
		assert.Eq(t, []string{"a.go:1:x", "a.go:2:y"}, rule.compiled.groupBy([]string{"a.go:1:x", "a.go:2:y"}))
	})
}

// TestOutputRuleHeadTail covers `head` and `tail`: the first and last N lines of
// the rule's result stay, what is in between becomes a marker line.
func TestOutputRuleHeadTail(t *testing.T) {
	lines := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	cases := []struct {
		name, rule string
		want       []string
	}{
		{
			name: "head and tail",
			rule: gatesOff + "    head: 3\n    tail: 2\n",
			want: []string{"a", "b", "c", "[... 5 lines omitted ...]", "i", "j"},
		},
		{
			name: "head only",
			rule: gatesOff + "    head: 4\n",
			want: []string{"a", "b", "c", "d", "[... 6 lines omitted ...]"},
		},
		{
			name: "tail only",
			rule: gatesOff + "    tail: 3\n",
			want: []string{"[... 7 lines omitted ...]", "h", "i", "j"},
		},
		{
			name: "nothing is cut when it already fits",
			rule: gatesOff + "    head: 12\n    tail: 5\n",
			want: lines,
		},
		{
			name: "no head and no tail changes nothing",
			rule: gatesOff + "    tail: 0\n",
			want: lines,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule := newTestRule(t, tc.rule)
			assert.Eq(t, tc.want, rule.compiled.headTail(lines))
		})
	}

	t.Run("the summary line is appended after head and tail", func(t *testing.T) {
		m, _ := newTestMinimizer(t, gatesOff+
			"    head: 2\n    tail: 1\n    count: {rows: '^row'}\n    summary: 'kept {rows} row(s)'\n")
		out := strings.Repeat("row of output\n", 20) + "last line\n[exit code: 0]"
		tidy, ok := m.Minimize(shellCall("fake tool"), out)
		assert.Require(t, assert.True(t, ok))
		assert.StrContains(t, tidy, "lines omitted")
		assert.StrContains(t, tidy, "kept 20 row(s)")
	})
}

// TestOutputRuleMatchOutput covers `match_output`: a rule only applies when the
// output holds that pattern too.
func TestOutputRuleMatchOutput(t *testing.T) {
	m, _ := newTestMinimizer(t, gatesOff+
		"    match_output: '(?m)^@@ '\n    drop: ['^\\+']\n")
	t.Run("applies when the output matches", func(t *testing.T) {
		tidy, ok := m.Minimize(shellCall("fake diff"), "@@ -1 +1 @@\n+a\n-b\n[exit code: 0]")
		assert.Require(t, assert.True(t, ok))
		assert.Eq(t, "@@ -1 +1 @@\n-b\n[exit code: 0]", tidy)
	})
	t.Run("stays out when it does not", func(t *testing.T) {
		_, ok := m.Minimize(shellCall("fake diff"), "+a\n+b\n+c\n+d\n[exit code: 0]")
		assert.False(t, ok)
	})
}

// TestOutputRuleGates covers min_bytes and min_saving: a rule may ask for a
// lower gate than the global one, the most permissive matching rule wins, and
// without a matching rule the defaults stand.
func TestOutputRuleGates(t *testing.T) {
	num, flt := func(v int) *int { return &v }, func(v float64) *float64 { return &v }
	t.Run("no rule: the globals", func(t *testing.T) {
		bytes, saving := ruleGates(nil)
		assert.Eq(t, minimizeMinBytes, bytes)
		assert.Eq(t, minimizeMinSaving, saving)
	})
	t.Run("one rule lowers byte and saving gate", func(t *testing.T) {
		bytes, saving := ruleGates([]OutputRule{{MinBytes: num(200), MinSaving: flt(0.05)}})
		assert.Eq(t, 200, bytes)
		assert.Eq(t, 0.05, saving)
	})
	t.Run("the most permissive of several rules wins", func(t *testing.T) {
		bytes, saving := ruleGates([]OutputRule{
			{MinBytes: num(300), MinSaving: flt(0.1)},
			{MinBytes: num(200), MinSaving: flt(0.05)},
			{Name: "no gate of its own"},
		})
		assert.Eq(t, 200, bytes)
		assert.Eq(t, 0.05, saving)
	})
	t.Run("a rule cannot raise a gate", func(t *testing.T) {
		bytes, saving := ruleGates([]OutputRule{{MinBytes: num(5000), MinSaving: flt(0.9)}})
		assert.Eq(t, minimizeMinBytes, bytes)
		assert.Eq(t, minimizeMinSaving, saving)
	})
	t.Run("0 means no gate at all", func(t *testing.T) {
		bytes, saving := ruleGates([]OutputRule{{MinBytes: num(0), MinSaving: flt(0)}})
		assert.Eq(t, 0, bytes)
		assert.Eq(t, 0.0, saving)
	})

	t.Run("end to end: below the global byte gate, above the rule's", func(t *testing.T) {
		// The global gate is 600 bytes and leaves this alone; the rule asking for
		// 200 bytes tidies it.
		out := strings.Repeat("noise line of some length\n", 15) + "[exit code: 0]"
		assert.True(t, len(out) > 200 && len(out) < minimizeMinBytes)
		m, _ := newTestMinimizer(t, "    min_bytes: 200\n    drop: ['^noise line of some length$']\n")
		tidy, ok := m.Minimize(shellCall("fake tool"), out)
		assert.Require(t, assert.True(t, ok))
		assert.Eq(t, "[exit code: 0]", tidy)

		m, _ = newTestMinimizer(t, "    drop: ['^noise line of some length$']\n")
		_, ok = m.Minimize(shellCall("fake tool"), out)
		assert.False(t, ok)
	})

	t.Run("end to end: saving below the global share but above the rule's", func(t *testing.T) {
		// Dropping one of 40 lines saves a couple of percent: too little for the
		// default 20%, enough for a rule asking for 1%.
		var b strings.Builder
		b.WriteString("removable line\n")
		for i := 0; i < 39; i++ {
			fmt.Fprintf(&b, "kept line %d of ordinary output\n", i)
		}
		out := b.String() + "[exit code: 0]"
		assert.True(t, len(out) > minimizeMinBytes)
		m, _ := newTestMinimizer(t, "    min_bytes: 0\n    drop: ['^removable line$']\n")
		_, ok := m.Minimize(shellCall("fake tool"), out)
		assert.False(t, ok)

		m, _ = newTestMinimizer(t, "    min_bytes: 0\n    min_saving: 0.01\n    drop: ['^removable line$']\n")
		tidy, ok := m.Minimize(shellCall("fake tool"), out)
		assert.Require(t, assert.True(t, ok))
		assert.NotContains(t, tidy, "removable")
	})
}

// TestOutputRuleNeverLonger: a rule that reshapes more than it shortens is not
// applied at all - the model never gets a longer result than the command
// printed.
func TestOutputRuleNeverLonger(t *testing.T) {
	m, _ := newTestMinimizer(t, gatesOff+"    group_by: '^([^:]+):'\n")
	// Grouping a one character path adds more than it drops.
	_, ok := m.Minimize(shellCall("fake grep"), "a:1:x\na:2:y\n[exit code: 0]")
	assert.False(t, ok)
}

// TestOutputRuleFieldErrors: a broken new field is a rule file error, reported
// with the rule's name.
func TestOutputRuleFieldErrors(t *testing.T) {
	cases := []struct{ name, rule, want string }{
		{
			name: "replace without a pattern",
			rule: "    replace:\n      - with: 'x'\n",
			want: "replace: pattern is required",
		},
		{
			name: "replace with a broken regexp",
			rule: "    replace:\n      - pattern: '(unclosed'\n        with: 'x'\n",
			want: "replace \"(unclosed\"",
		},
		{
			name: "group_by without a capture group",
			rule: "    group_by: '^\\S+:'\n",
			want: "needs a capture group",
		},
		{
			name: "group_by with a broken regexp",
			rule: "    group_by: '([^'\n",
			want: "group_by \"([^\"",
		},
		{
			name: "negative head",
			rule: "    head: -1\n",
			want: "head and tail must not be negative",
		},
		{
			name: "negative min_bytes",
			rule: "    min_bytes: -1\n",
			want: "min_bytes must not be negative",
		},
		{
			name: "min_saving above 1",
			rule: "    min_saving: 1.5\n",
			want: "min_saving must be between 0 and 1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseOutputRules([]byte("rules:\n  - name: fake\n    match: '^fake\\b'\n" + tc.rule))
			assert.Require(t, assert.NotNil(t, err))
			assert.StrContains(t, err.Error(), "fake:")
			assert.StrContains(t, err.Error(), tc.want)
		})
	}
}
