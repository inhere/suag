package native

import (
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// gitLogDefault is the default `git log -3` output of this repository: a merge
// with a decoration, a plain commit and one with a body.
const gitLogDefault = "commit ac377f140b3ee11b4f4bccac75c6df13fa5e4a75 (HEAD -> main)\n" +
	"Merge: 1fe2662 1fb3dc5\n" +
	"Author: T <t@t.t>\n" +
	"Date:   Wed Oct 14 18:15:20 2026 +0800\n" +
	"\n" +
	"    Merge branch 'feature'\n" +
	"\n" +
	"commit 1fb3dc5ccb4ac49a8c836c7be2d07a4f0b1edf24 (feature)\n" +
	"Author: T <t@t.t>\n" +
	"Date:   Wed Oct 14 18:15:20 2026 +0800\n" +
	"\n" +
	"    feat: add feat.txt\n" +
	"\n" +
	"commit 1fe2662d603543fe5fe3139bf5f2eba9d3ae1166\n" +
	"Author: somebody else <other@example.com>\n" +
	"Date:   Wed Oct 14 18:12:04 2026 +0800\n" +
	"\n" +
	"    init\n" +
	"    \n" +
	"    A body paragraph that stays as it is.\n" +
	"[exit code: 0]"

// gitDiffMixed is `git diff` of a go.sum change, an ordinary source file and a
// vendored file, as git prints it.
const gitDiffMixed = "diff --git a/go.sum b/go.sum\n" +
	"index 73a0378..8afc798 100644\n" +
	"--- a/go.sum\n" +
	"+++ b/go.sum\n" +
	"@@ -38,3 +38,5 @@ github.com/pkg37/mod v1.0.37 h1:AAAAAAAA\n" +
	" github.com/pkg38/mod v1.0.38 h1:AAAAAAAA\n" +
	" github.com/pkg39/mod v1.0.39 h1:AAAAAAAA\n" +
	" github.com/pkg40/mod v1.0.40 h1:AAAAAAAA\n" +
	"+github.com/new/dep v1.2.3 h1:BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=\n" +
	"+github.com/new/dep v1.2.3/go.mod h1:CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC=\n" +
	"diff --git a/pkg/sub/a.go b/pkg/sub/a.go\n" +
	"index 2a4f7cf..e594678 100644\n" +
	"--- a/pkg/sub/a.go\n" +
	"+++ b/pkg/sub/a.go\n" +
	"@@ -1,3 +1,3 @@\n" +
	" package sub\n" +
	" \n" +
	"-func A() int { return 1 }\n" +
	"+func A() int { return 2 }\n" +
	"diff --git a/vendor/github.com/x/x.go b/vendor/github.com/x/x.go\n" +
	"index 823aafd..df44d0f 100644\n" +
	"--- a/vendor/github.com/x/x.go\n" +
	"+++ b/vendor/github.com/x/x.go\n" +
	"@@ -1 +1,2 @@\n" +
	" package x\n" +
	"+func B() int { return 3 }\n" +
	"[exit code: 0]"

// gitStatusLong is `git status` in its long form (with the hints for the next
// step and the file list).
const gitStatusLong = "On branch main\n" +
	"Changes not staged for commit:\n" +
	"  (use \"git add <file>...\" to update what will be committed)\n" +
	"  (use \"git restore <file>...\" to discard changes in working directory)\n" +
	"\tmodified:   go.sum\n" +
	"\tmodified:   pkg/sub/a.go\n" +
	"\tmodified:   vendor/github.com/x/x.go\n" +
	"\n" +
	"Untracked files:\n" +
	"  (use \"git add <file>...\" to include in what will be committed)\n" +
	"\ttmp/\n" +
	"\n" +
	"no changes added to commit (use \"git add\" and/or \"git commit -a\")\n" +
	"[exit code: 0]"

// TestBuiltinGitRules runs the git rules over output as the tools print it: a
// rule only acts when the output has the shape it was written for, and the
// result is never longer than the original.
func TestBuiltinGitRules(t *testing.T) {
	cases := []struct {
		name, command, output string
		want, notWant         []string
	}{
		{
			name:    "git log: one line per commit, merges and decorations kept",
			command: "git log -3",
			output:  gitLogDefault,
			want: []string{
				"ac377f1 (HEAD -> main)  Wed Oct 14 18:15:20 2026 +0800  T  Merge: 1fe2662 1fb3dc5  Merge branch 'feature'",
				"1fb3dc5 (feature)  Wed Oct 14 18:15:20 2026 +0800  T  feat: add feat.txt",
				"1fe2662  Wed Oct 14 18:12:04 2026 +0800  somebody else  init",
				"A body paragraph that stays as it is.",
			},
			notWant: []string{"commit ac377f140b3ee11b4f4bccac75c6df13fa5e4a75", "Author: T <t@t.t>", "Date:   Wed"},
		},
		{
			name:    "git log --oneline is already compact",
			command: "git log --oneline -6",
			output: repeatLines(6, "%d0f5b1c docs: something worth a commit subject line here") +
				repeatLines(4, "%d2d9e34 fix: another commit subject line for the log") + "[exit code: 0]",
		},
		{
			name:    "git log --graph is not touched",
			command: "git log --graph -3",
			output: "* commit ac377f140b3ee11b4f4bccac75c6df13fa5e4a75 (HEAD -> main)\n" +
				"* Merge: 1fe2662 1fb3dc5\n* Author: T <t@t.t>\n* Date:   Wed Oct 14 18:15:20 2026 +0800\n*\n" +
				"*     Merge branch 'feature'\n*     and some more words to grow the output\n[exit code: 0]",
		},
		{
			name:    "git diff: lock and generated files fold, ordinary ones stay",
			command: "git diff",
			output:  gitDiffMixed,
			want: []string{
				"已折叠 go.sum 的 10 行改动",
				"-func A() int { return 1 }\n+func A() int { return 2 }",
				"已折叠 vendor/github.com/x/x.go 的 7 行改动",
			},
			notWant: []string{"73a0378", "+github.com/new/dep", "package x"},
		},
		{
			name:    "git diff --stat has nothing to fold",
			command: "git diff --stat",
			output: " go.sum                   | 2 ++\n" +
				" pkg/sub/a.go             | 2 +-\n" +
				" vendor/github.com/x/x.go | 1 +\n" +
				" 3 files changed, 4 insertions(+), 1 deletion(-)\n" +
				"[exit code: 0]",
		},
		{
			name:    "git status: hints go, the file list stays",
			command: "git status",
			output:  gitStatusLong,
			want: []string{
				"On branch main",
				"Changes not staged for commit:\n\tmodified:   go.sum\n\tmodified:   pkg/sub/a.go\n\tmodified:   vendor/github.com/x/x.go",
				"Untracked files:\n\ttmp/",
			},
			notWant: []string{"(use \"git add", "(use \"git restore", "no changes added to commit"},
		},
		{
			name:    "git status --short has no hints to drop",
			command: "git status --short",
			output: repeatLines(30, " M pkg/agentcore/tools/native/file%d.go") +
				repeatLines(10, "?? tmp/scratch%d.txt") + "[exit code: 0]",
		},
		{
			name:    "git show: a commit with its lock file diff",
			command: "git show 5b2e84c",
			output: strings.Replace(gitDiffMixed, "diff --git a/pkg/sub/a.go", "diff --git a/package-lock.json b/package-lock.json", 1) +
				"[exit code: 0]",
			want: []string{"已折叠 go.sum", "已折叠 package-lock.json", "已折叠 vendor/"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tidy, ok := MinimizeToolOutput(shellCall(tc.command), tc.output)
			if len(tc.want) == 0 {
				assert.False(t, ok) // a rule that cannot improve this output stays out
				return
			}
			assert.Require(t, assert.True(t, ok))
			assert.True(t, len(tidy) < len(tc.output))
			for _, w := range tc.want {
				assert.StrContains(t, tidy, w)
			}
			for _, w := range tc.notWant {
				assert.NotContains(t, tidy, w)
			}
		})
	}
}

// TestBuiltinGrepRule runs the grep rule over rg output: the hits of one file
// are grouped under one path line and a very long hit is cut.
func TestBuiltinGrepRule(t *testing.T) {
	long := "pkg/agentcore/tools/native/minimize.go:60:\tmatched := m.matchingRules(command, content) // " + strings.Repeat("x", 400)
	cases := []struct {
		name, command, output string
		want, notWant         []string
	}{
		{
			name:    "rg: matches grouped by file",
			command: `rg -n "func|package" ./...`,
			output: repeatLines(20, "pkg/agentcore/tools/native/output_rules.go:%d:\treturn r.compiled.apply(lines, r.Summary)") +
				repeatLines(6, "internal/personal/runner.go:%d:\tMinimizeOutput func(call agentcore.ToolCall, content string)") +
				long,
			want: []string{
				"pkg/agentcore/tools/native/output_rules.go:\n  0:\treturn r.compiled.apply(lines, r.Summary)",
				"internal/personal/runner.go:\n  0:\tMinimizeOutput func",
				"…", // the long hit is cut
			},
			notWant: []string{
				"output_rules.go:1:\treturn", // the path is not repeated on every hit
				strings.Repeat("x", 400),
			},
		},
		{
			name:    "grep: a windows path keeps its drive letter",
			command: `grep -rn "func" pkg`,
			output: repeatLines(12, `D:\work\suga\pkg\a.go:%d:func A() int`) +
				repeatLines(8, `D:\work\suga\pkg\b.go:%d:func B() int`),
			want:    []string{`D:\work\suga\pkg\a.go:`, `  0:func A() int`},
			notWant: []string{`D:\work\suga\pkg\a.go:1:func`},
		},
		{
			name:    "grep -c prints counts, not matches",
			command: "grep -c func ./...",
			output:  repeatLines(40, "pkg/agentcore/tools/native/file%d.go:12"),
		},
		{
			name:    "grep -l prints file names only",
			command: "grep -rl func ./...",
			output:  repeatLines(40, "pkg/agentcore/tools/native/very_long_file_name_here_%d.go"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := tc.output + "[exit code: 0]"
			tidy, ok := MinimizeToolOutput(shellCall(tc.command), out)
			if len(tc.want) == 0 {
				assert.False(t, ok)
				return
			}
			assert.Require(t, assert.True(t, ok))
			assert.True(t, len(tidy) < len(out))
			for _, w := range tc.want {
				assert.StrContains(t, tidy, w)
			}
			for _, w := range tc.notWant {
				assert.NotContains(t, tidy, w)
			}
		})
	}
}
