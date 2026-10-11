package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

// Task is one evaluation task loaded from testdata/tasks/<name>/task.yaml.
type Task struct {
	Name     string     `yaml:"name"`
	Prompt   string     `yaml:"prompt"`
	Prompts  []string   `yaml:"prompts"` // multi-turn: one session, sent in order; exclusive with prompt
	Mode     string     `yaml:"mode"`    // read-only | allow-write
	FakeMode string     `yaml:"fake_mode"`
	Generate []Generate `yaml:"generate"`
	Checks   []Check    `yaml:"checks"`

	dir string // task directory (holds fixture/)
}

// Generate describes a file produced at run time inside the workspace, so big
// fixtures need not be committed. Each of Count lines is Line with {i}
// replaced by the 1-based line number; Inserts replace line At verbatim.
type Generate struct {
	Path    string   `yaml:"path"`
	Line    string   `yaml:"line"`
	Count   int      `yaml:"count"`
	Inserts []Insert `yaml:"inserts"`
}

// Insert replaces the generated line number At (1-based) with Text.
type Insert struct {
	At   int    `yaml:"at"`
	Text string `yaml:"text"`
}

// Check is one pass/fail assertion. Types: file_contains, file_equals,
// file_not_contains (use path+text) and answer_contains (text only).
type Check struct {
	Type string `yaml:"type"`
	Path string `yaml:"path"`
	Text string `yaml:"text"`
}

// CheckResult is the recorded outcome of one Check.
type CheckResult struct {
	Type   string `json:"type"`
	Path   string `json:"path,omitempty"`
	Text   string `json:"text"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

func loadTask(dir string) (*Task, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "task.yaml"))
	if err != nil {
		return nil, err
	}
	var t Task
	if err := yaml.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	if t.Name == "" || (t.Prompt == "") == (len(t.Prompts) == 0) {
		return nil, fmt.Errorf("%s: name and exactly one of prompt / prompts are required", dir)
	}
	for _, p := range t.Prompts {
		if strings.TrimSpace(p) == "" {
			return nil, fmt.Errorf("%s: empty entry in prompts", dir)
		}
	}
	switch t.Mode {
	case "":
		t.Mode = "read-only"
	case "read-only", "allow-write":
	default:
		return nil, fmt.Errorf("%s: unknown mode %q", dir, t.Mode)
	}
	if t.FakeMode == "" {
		t.FakeMode = "text"
	}
	t.dir = dir
	return &t, nil
}

// Turns returns the prompts to send in order (a single-prompt task has one).
// answer_contains checks apply to the answer of the last turn.
func (t *Task) Turns() []string {
	if len(t.Prompts) > 0 {
		return t.Prompts
	}
	return []string{t.Prompt}
}

func loadTasks(root, pattern string) ([]*Task, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var tasks []*Task
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if ok, err := filepath.Match(pattern, e.Name()); err != nil {
			return nil, err
		} else if !ok {
			continue
		}
		t, err := loadTask(filepath.Join(root, e.Name()))
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

// safeJoin joins a task-relative path under ws and rejects escapes.
func safeJoin(ws, rel string) (string, error) {
	p := filepath.Join(ws, filepath.FromSlash(rel))
	r, err := filepath.Rel(ws, p)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the workspace", rel)
	}
	return p, nil
}

func (g Generate) write(ws string) error {
	p, err := safeJoin(ws, g.Path)
	if err != nil {
		return err
	}
	over := map[int]string{}
	for _, in := range g.Inserts {
		over[in.At] = in.Text
	}
	var b strings.Builder
	for i := 1; i <= g.Count; i++ {
		if s, ok := over[i]; ok {
			b.WriteString(s)
		} else {
			b.WriteString(strings.ReplaceAll(g.Line, "{i}", fmt.Sprint(i)))
		}
		b.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(b.String()), 0o644)
}

// runChecks evaluates every check against the workspace and the final answer.
func runChecks(ws, answer string, checks []Check) []CheckResult {
	out := make([]CheckResult, 0, len(checks))
	for _, c := range checks {
		r := CheckResult{Type: c.Type, Path: c.Path, Text: c.Text}
		if c.Type == "answer_contains" {
			r.Passed = strings.Contains(answer, c.Text)
			out = append(out, r)
			continue
		}
		p, err := safeJoin(ws, c.Path)
		var data []byte
		if err == nil {
			data, err = os.ReadFile(p)
		}
		if err != nil {
			r.Detail = err.Error()
			out = append(out, r)
			continue
		}
		s := string(data)
		switch c.Type {
		case "file_contains":
			r.Passed = strings.Contains(s, c.Text)
		case "file_not_contains":
			r.Passed = !strings.Contains(s, c.Text)
		case "file_equals":
			r.Passed = strings.TrimRight(s, "\r\n") == strings.TrimRight(c.Text, "\r\n")
		default:
			r.Detail = "unknown check type"
		}
		out = append(out, r)
	}
	return out
}

func allPassed(rs []CheckResult) bool {
	for _, r := range rs {
		if !r.Passed {
			return false
		}
	}
	return true
}
