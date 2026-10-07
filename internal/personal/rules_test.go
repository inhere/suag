package personal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func putRule(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRulesOrderAndIncludes(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	putRule(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	putRule(t, filepath.Join(root, "AGENTS.md"), "root\n@part.md\n")
	putRule(t, filepath.Join(root, "part.md"), "part\n@nested.md\n")
	putRule(t, filepath.Join(root, "nested.md"), "never-expand")
	putRule(t, filepath.Join(child, "AGENTS.md"), "child")
	putRule(t, filepath.Join(child, "CLAUDE.md"), "@AGENTS.md\n")
	putRule(t, filepath.Join(child, "below", "AGENTS.md"), "below")
	home := t.TempDir()
	putRule(t, filepath.Join(home, "AGENTS.md"), "user")
	rules, hash, err := LoadRules(child, home, false)
	assert.NoErr(t, err)
	assert.Eq(t, true, strings.Index(rules, "user") < strings.Index(rules, "root"))
	assert.Eq(t, true, strings.Index(rules, "root") < strings.Index(rules, "child"))
	assert.Eq(t, true, strings.Contains(rules, "part\n@nested.md"))
	assert.Eq(t, false, strings.Contains(rules, "never-expand"))
	assert.Eq(t, false, strings.Contains(rules, "below"))
	assert.Eq(t, 1, strings.Count(rules, "\nchild"))
	assert.Eq(t, true, hash != "")
	userOnly, _, err := LoadRules(child, home, true)
	assert.NoErr(t, err)
	assert.Eq(t, true, strings.Contains(userOnly, "user"))
	assert.Eq(t, false, strings.Contains(userOnly, "root"))
}

func TestLoadRulesFallsBackToClaudeInNonGitCwd(t *testing.T) {
	parent := t.TempDir()
	cwd := filepath.Join(parent, "cwd")
	putRule(t, filepath.Join(parent, "AGENTS.md"), "outside cwd")
	putRule(t, filepath.Join(cwd, "CLAUDE.md"), "claude rule")
	rules, _, err := LoadRules(cwd, "", false)
	assert.NoErr(t, err)
	assert.Eq(t, true, strings.Contains(rules, "claude rule"))
	assert.Eq(t, false, strings.Contains(rules, "outside cwd"))
}

func TestLoadRulesLimitsAndCycle(t *testing.T) {
	root := t.TempDir()
	putRule(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	putRule(t, filepath.Join(root, "AGENTS.md"), "@AGENTS.md\n")
	rules, _, err := LoadRules(root, t.TempDir(), false)
	assert.NoErr(t, err)
	assert.Eq(t, true, strings.Contains(rules, "[循环引用"))
	putRule(t, filepath.Join(root, "AGENTS.md"), strings.Repeat("a", 65*1024))
	rules, _, err = LoadRules(root, t.TempDir(), false)
	assert.NoErr(t, err)
	assert.Eq(t, true, strings.Contains(rules, "[规则已截断"))
	assert.Eq(t, false, strings.Contains(rules, strings.Repeat("a", 65*1024)))
	home := t.TempDir()
	putRule(t, filepath.Join(home, "AGENTS.md"), strings.Repeat("u", 60*1024))
	putRule(t, filepath.Join(root, "AGENTS.md"), "@extra.md\n"+strings.Repeat("p", 60*1024))
	putRule(t, filepath.Join(root, "extra.md"), strings.Repeat("e", 60*1024))
	rules, _, err = LoadRules(root, home, false)
	assert.NoErr(t, err)
	assert.Eq(t, true, strings.Contains(rules, "[规则已截断: 总量超过 128KB]"))
}
