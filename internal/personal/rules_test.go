package personal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

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

func TestLoadRulesReadsBothProjectRuleFiles(t *testing.T) {
	root := t.TempDir()
	putRule(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	putRule(t, filepath.Join(root, "AGENTS.md"), "agents rules\n")
	putRule(t, filepath.Join(root, "CLAUDE.md"), "claude rules\n@workspace.md\n")
	putRule(t, filepath.Join(root, "workspace.md"), "workspace rules\n")

	rules, _, err := LoadRules(root, "", false)
	assert.NoErr(t, err)
	assert.Eq(t, true, strings.Contains(rules, "agents rules"))
	assert.Eq(t, true, strings.Contains(rules, "claude rules"))
	assert.Eq(t, true, strings.Contains(rules, "workspace rules"))
	assert.Eq(t, false, strings.Contains(rules, "@workspace.md"))
}

func TestLoadRulesDeduplicatesIncludesAcrossProjectFiles(t *testing.T) {
	root := t.TempDir()
	putRule(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	putRule(t, filepath.Join(root, "AGENTS.md"), "@workspace.md\n")
	putRule(t, filepath.Join(root, "CLAUDE.md"), "claude marker\n@workspace.md\n")
	putRule(t, filepath.Join(root, "workspace.md"), "workspace rules\n")

	rules, _, err := LoadRules(root, "", false)
	assert.NoErr(t, err)
	assert.Eq(t, true, strings.Contains(rules, "claude marker"))
	assert.Eq(t, 1, strings.Count(rules, "workspace rules"))
}

func TestLoadRulesExpandsIncludeWithExplanation(t *testing.T) {
	root := t.TempDir()
	putRule(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	putRule(t, filepath.Join(root, "AGENTS.md"), "agents rules\n")
	putRule(t, filepath.Join(root, "CLAUDE.md"), "@workspace.md 需要先加载阅读。\n")
	putRule(t, filepath.Join(root, "workspace.md"), "workspace rules\n")

	rules, _, err := LoadRules(root, "", false)
	assert.NoErr(t, err)
	assert.Eq(t, true, strings.Contains(rules, "workspace rules"))
	assert.Eq(t, false, strings.Contains(rules, "@workspace.md"))
}

func TestLoadRulesSkipsClaudeReferenceToAgentsAlreadyRead(t *testing.T) {
	root := t.TempDir()
	putRule(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	putRule(t, filepath.Join(root, "AGENTS.md"), "agents rules\n")
	putRule(t, filepath.Join(root, "CLAUDE.md"), "@AGENTS.md\n")

	rules, _, err := LoadRules(root, "", false)
	assert.NoErr(t, err)
	assert.Eq(t, 1, strings.Count(rules, "agents rules"))
}

func TestLoadRulesSkipsClaudeWithIdenticalContent(t *testing.T) {
	root := t.TempDir()
	putRule(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	putRule(t, filepath.Join(root, "AGENTS.md"), "shared rules\n")
	putRule(t, filepath.Join(root, "CLAUDE.md"), "shared rules\n")

	rules, _, err := LoadRules(root, "", false)
	assert.NoErr(t, err)
	assert.Eq(t, 1, strings.Count(rules, "shared rules"))
}

func TestLoadRulesKeepsSameIncludeTextFromDifferentDirectories(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	putRule(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	putRule(t, filepath.Join(home, "AGENTS.md"), "@workspace.md\n")
	putRule(t, filepath.Join(home, "workspace.md"), "user workspace rules\n")
	putRule(t, filepath.Join(root, "AGENTS.md"), "@workspace.md\n")
	putRule(t, filepath.Join(root, "workspace.md"), "project workspace rules\n")

	rules, _, err := LoadRules(root, home, false)
	assert.NoErr(t, err)
	assert.Eq(t, true, strings.Contains(rules, "user workspace rules"))
	assert.Eq(t, true, strings.Contains(rules, "project workspace rules"))
}

func TestLoadRulesIncludeFallbacks(t *testing.T) {
	newRoot := func(t *testing.T, content string) string {
		root := t.TempDir()
		putRule(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
		putRule(t, filepath.Join(root, "AGENTS.md"), content)
		return root
	}

	t.Run("fenced code block is not include", func(t *testing.T) {
		root := newRoot(t, "```java\n@Test\n@Override\n```\n~~~\n@Foo\n~~~\n")
		rules, _, err := LoadRules(root, t.TempDir(), false)
		assert.NoErr(t, err)
		assert.StrContains(t, rules, "@Test\n@Override")
		assert.StrContains(t, rules, "@Foo")
	})

	t.Run("missing target keeps original line", func(t *testing.T) {
		root := newRoot(t, "@AGENTS-missing.md\n@某人 负责发布\nafter\n")
		rules, _, err := LoadRules(root, t.TempDir(), false)
		assert.NoErr(t, err)
		assert.StrContains(t, rules, "@AGENTS-missing.md")
		assert.StrContains(t, rules, "@某人 负责发布")
		assert.StrContains(t, rules, "after")
	})

	t.Run("directory target keeps original line", func(t *testing.T) {
		root := newRoot(t, "@docs/\nafter\n")
		assert.NoErr(t, os.MkdirAll(filepath.Join(root, "docs"), 0755))
		rules, _, err := LoadRules(root, t.TempDir(), false)
		assert.NoErr(t, err)
		assert.StrContains(t, rules, "@docs/")
		assert.StrContains(t, rules, "after")
	})
}

func TestLoadRulesTruncatesOnUTF8Boundary(t *testing.T) {
	root := t.TempDir()
	putRule(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	// 64KB 不是 3 的倍数，按字节切必然落在汉字中间。
	putRule(t, filepath.Join(root, "AGENTS.md"), strings.Repeat("中", 30000))
	rules, _, err := LoadRules(root, t.TempDir(), false)
	assert.NoErr(t, err)
	assert.StrContains(t, rules, "单文件超过 64KB")
	assert.True(t, utf8.ValidString(rules))

	// 总量截断：home 占 64KB，项目文件用汉字填超剩余额度。
	home := t.TempDir()
	putRule(t, filepath.Join(home, "AGENTS.md"), strings.Repeat("a", 64*1024-1))
	putRule(t, filepath.Join(root, "AGENTS.md"), strings.Repeat("中", 30000))
	rules, _, err = LoadRules(root, home, false)
	assert.NoErr(t, err)
	assert.True(t, utf8.ValidString(rules))
}

func TestLoadRulesTotalLimitMarker(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	putRule(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	putRule(t, filepath.Join(home, "AGENTS.md"), strings.Repeat("u", 64*1024))

	putRule(t, filepath.Join(root, "extra.md"), strings.Repeat("p", 64*1024))

	t.Run("exactly 128KB has no marker", func(t *testing.T) {
		putRule(t, filepath.Join(root, "AGENTS.md"), "@extra.md\n")
		rules, _, err := LoadRules(root, home, false)
		assert.NoErr(t, err)
		assert.StrNotContains(t, rules, "总量超过 128KB")
	})

	t.Run("one byte over adds marker", func(t *testing.T) {
		putRule(t, filepath.Join(root, "AGENTS.md"), "@extra.md\nx")
		rules, _, err := LoadRules(root, home, false)
		assert.NoErr(t, err)
		assert.StrContains(t, rules, "总量超过 128KB")
	})
}

func TestLoadRulesHashIgnoresCheckoutLocation(t *testing.T) {
	build := func(content string) (string, string) {
		root := t.TempDir()
		putRule(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
		putRule(t, filepath.Join(root, "AGENTS.md"), "root\n@part.md\n")
		putRule(t, filepath.Join(root, "part.md"), content)
		sub := filepath.Join(root, "sub")
		putRule(t, filepath.Join(sub, "AGENTS.md"), "sub")
		return root, sub
	}
	home1, home2 := t.TempDir(), t.TempDir()
	putRule(t, filepath.Join(home1, "AGENTS.md"), "user")
	putRule(t, filepath.Join(home2, "AGENTS.md"), "user")

	_, sub1 := build("part-v1")
	_, sub2 := build("part-v1")
	_, sub3 := build("part-v2")
	rules1, h1, err := LoadRules(sub1, home1, false)
	assert.NoErr(t, err)
	rules2, h2, err := LoadRules(sub2, home2, false)
	assert.NoErr(t, err)
	_, h3, err := LoadRules(sub3, home1, false)
	assert.NoErr(t, err)

	t.Run("same content at different roots", func(t *testing.T) {
		assert.Eq(t, h1, h2)
		// The prompt text still shows real paths; only the hash is neutral.
		assert.NotEq(t, rules1, rules2)
	})
	t.Run("content change changes hash", func(t *testing.T) {
		assert.NotEq(t, h1, h3)
	})
}
