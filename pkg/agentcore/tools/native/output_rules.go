package native

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

//go:embed output_rules.yaml
var builtinOutputRules []byte

// OutputRule tidies the output of the shell commands it matches; see
// output_rules.yaml for the fields. Rules are data so new tools and
// languages are added without code (omp's shell minimizer works the same
// way).
type OutputRule struct {
	Name    string   `yaml:"name"`
	Enabled *bool    `yaml:"enabled"` // nil = on
	Match   string   `yaml:"match"`
	Drop    []string `yaml:"drop"`
	Block   struct {
		Start string `yaml:"start"`
		Body  string `yaml:"body"`
		Pass  string `yaml:"pass"`
		Fail  string `yaml:"fail"`
	} `yaml:"block"`
	Failure []string          `yaml:"failure"`
	Count   map[string]string `yaml:"count"`
	Sum     map[string]string `yaml:"sum"`
	Fold    []FoldBlock       `yaml:"fold"`
	Summary string            `yaml:"summary"`

	compiled *compiledRule
}

// FoldBlock collapses a run of lines repeating the same noise into one line:
// start matches its first line, body every following line of the run, and text
// is what replaces the run ({lines} = the number of lines folded; empty text
// drops the whole run). Maven's effective-model warnings are the case it was
// added for.
type FoldBlock struct {
	Start string `yaml:"start"`
	Body  string `yaml:"body"`
	Text  string `yaml:"text"`
}

type compiledRule struct {
	match                   *regexp.Regexp
	drop, failure           []*regexp.Regexp
	start, body, pass, fail *regexp.Regexp
	countNames              []string
	counts                  map[string]*regexp.Regexp
	sums                    map[string]*regexp.Regexp
	folds                   []compiledFold
}

// compiledFold is a compiled FoldBlock.
type compiledFold struct {
	start, body *regexp.Regexp
	text        string
}

type outputRuleFile struct {
	Rules []OutputRule `yaml:"rules"`
}

// DefaultOutputRules returns the built-in rules (go test, cargo test, jest
// and vitest, pytest, maven, gradle).
func DefaultOutputRules() []OutputRule {
	rules, err := parseOutputRules(builtinOutputRules)
	if err != nil {
		panic("native: built-in output rules: " + err.Error())
	}
	return rules
}

// LoadOutputRules returns the built-in rules merged with the rule files in
// order (later files win): a rule with the name of an earlier one replaces
// it, a new name is appended. Missing files are skipped; a file that does not
// parse or holds a broken rule is reported and skipped as a whole.
func LoadOutputRules(paths ...string) ([]OutputRule, []error) {
	rules := DefaultOutputRules()
	var errs []error
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		extra, err := parseOutputRules(data)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			continue
		}
		rules = mergeOutputRules(rules, extra)
	}
	var out []OutputRule
	for _, r := range rules {
		if r.Enabled != nil && !*r.Enabled {
			continue
		}
		out = append(out, r)
	}
	return out, errs
}

func mergeOutputRules(base, extra []OutputRule) []OutputRule {
	for _, r := range extra {
		replaced := false
		for i := range base {
			if base[i].Name == r.Name {
				base[i], replaced = r, true
				break
			}
		}
		if !replaced {
			base = append(base, r)
		}
	}
	return base
}

// parseOutputRules decodes and compiles a rule file; every regex must
// compile and every enabled rule needs a name and a match.
func parseOutputRules(data []byte) ([]OutputRule, error) {
	var file outputRuleFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	for i := range file.Rules {
		r := &file.Rules[i]
		if r.Name == "" {
			return nil, fmt.Errorf("rules[%d]: name is required", i)
		}
		if r.Enabled != nil && !*r.Enabled {
			continue // only switches a rule of that name off
		}
		c, err := compileRule(r)
		if err != nil {
			return nil, fmt.Errorf("rule %s: %w", r.Name, err)
		}
		r.compiled = c
	}
	return file.Rules, nil
}

func compileRule(r *OutputRule) (*compiledRule, error) {
	if r.Match == "" {
		return nil, errors.New("match is required")
	}
	c := &compiledRule{counts: map[string]*regexp.Regexp{}, sums: map[string]*regexp.Regexp{}}
	var err error
	one := func(expr string) *regexp.Regexp {
		if expr == "" || err != nil {
			return nil
		}
		var re *regexp.Regexp
		if re, err = regexp.Compile(expr); err != nil {
			err = fmt.Errorf("%q: %w", expr, err)
		}
		return re
	}
	many := func(exprs []string) []*regexp.Regexp {
		var out []*regexp.Regexp
		for _, e := range exprs {
			if re := one(e); re != nil {
				out = append(out, re)
			}
		}
		return out
	}
	c.match = one(r.Match)
	c.drop, c.failure = many(r.Drop), many(r.Failure)
	c.start, c.body, c.pass, c.fail = one(r.Block.Start), one(r.Block.Body), one(r.Block.Pass), one(r.Block.Fail)
	for name, expr := range r.Count {
		c.counts[name] = one(expr)
		c.countNames = append(c.countNames, name)
	}
	// A summed value is rendered like a count: one name, one number.
	for name, expr := range r.Sum {
		c.sums[name] = one(expr)
		if _, counted := c.counts[name]; !counted {
			c.countNames = append(c.countNames, name)
		}
	}
	for _, f := range r.Fold {
		fold := compiledFold{start: one(f.Start), body: one(f.Body), text: f.Text}
		if fold.start != nil {
			c.folds = append(c.folds, fold)
		}
	}
	return c, err
}

// sumValue returns the number in the first capture group of re when the line
// matches and holds one; ok is false otherwise.
func sumValue(re *regexp.Regexp, line string) (int, bool) {
	m := re.FindStringSubmatch(line)
	if m == nil || len(m) < 2 {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(m[1]))
	if err != nil {
		return 0, false
	}
	return n, true
}

// apply runs the rule over lines: per-test blocks of passed tests and the
// drop lines go, everything else stays; when nothing failed and a count is
// positive, the summary line is appended.
func (c *compiledRule) apply(lines []string, summary string) []string {
	lines = c.foldNoise(lines)
	var out, pending []string
	counts := map[string]int{}
	failed, inPassed := false, false
	flush := func() {
		out = append(out, pending...)
		pending = pending[:0]
	}
	matchAny := func(res []*regexp.Regexp, l string) bool {
		for _, re := range res {
			if re.MatchString(l) {
				return true
			}
		}
		return false
	}
	for _, l := range lines {
		for name, re := range c.counts {
			if re != nil && re.MatchString(l) {
				counts[name]++
			}
		}
		for name, re := range c.sums {
			if re == nil {
				continue
			}
			if n, ok := sumValue(re, l); ok {
				counts[name] += n
			}
		}
		isFail := matchAny(c.failure, l)
		if isFail {
			failed = true
		}
		inBody := c.body != nil && c.body.MatchString(l)
		switch {
		case c.pass != nil && c.pass.MatchString(l):
			pending, inPassed = pending[:0], true // the passed test's own output goes too
			if matchAny(c.drop, l) {
				continue
			}
			out = append(out, l)
			continue
		case inBody && inPassed:
			continue
		case inBody && !isFail && c.start != nil:
			pending = append(pending, l) // decided by the test's result line
			continue
		}
		inPassed = false
		if c.fail != nil && c.fail.MatchString(l) {
			flush()
			out = append(out, l)
			continue
		}
		if !isFail && matchAny(c.drop, l) {
			if c.start != nil && c.start.MatchString(l) {
				flush() // a new test starts: an undecided block stays
			}
			continue
		}
		flush()
		out = append(out, l)
	}
	flush()
	if failed || summary == "" {
		return out
	}
	if text := renderSummary(summary, c.countNames, counts); text != "" {
		out = append(out, text)
	}
	return out
}

// foldNoise replaces every fold block - its start line and the following lines
// matching its body - with the block's text line. Rules without fold blocks
// return the lines unchanged.
func (c *compiledRule) foldNoise(lines []string) []string {
	if len(c.folds) == 0 {
		return lines
	}
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		found, n := c.foldAt(lines, i)
		if n == 0 {
			out = append(out, lines[i])
			i++
			continue
		}
		if found.text != "" {
			out = append(out, strings.ReplaceAll(found.text, "{lines}", strconv.Itoa(n)))
		}
		i += n
	}
	return out
}

// foldAt returns the first fold block whose start matches lines[i] and how many
// lines it swallows (0 when no block starts there). A following line joins the
// run while it matches the block's body.
func (c *compiledRule) foldAt(lines []string, i int) (compiledFold, int) {
	for _, fold := range c.folds {
		if !fold.start.MatchString(lines[i]) {
			continue
		}
		n := 1
		for fold.body != nil && i+n < len(lines) && fold.body.MatchString(lines[i+n]) {
			n++
		}
		return fold, n
	}
	return compiledFold{}, 0
}

var optionalPart = regexp.MustCompile(`\[([^\[\]]*)\]`)

// renderSummary fills the {name} counts into summary; a [part] whose counts
// are all 0 is left out (the brackets of a kept part go). "" when every count
// is 0.
func renderSummary(summary string, names []string, counts map[string]int) string {
	positive := false
	for _, name := range names {
		positive = positive || counts[name] > 0
	}
	if !positive {
		return ""
	}
	fill := func(text string) (string, bool) {
		any := false
		for _, name := range names {
			if strings.Contains(text, "{"+name+"}") {
				any = any || counts[name] > 0
				text = strings.ReplaceAll(text, "{"+name+"}", fmt.Sprint(counts[name]))
			}
		}
		return text, any
	}
	summary = optionalPart.ReplaceAllStringFunc(summary, func(part string) string {
		if text, any := fill(part[1 : len(part)-1]); any {
			return text
		}
		return ""
	})
	text, _ := fill(summary)
	return text
}
