package wiring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore/tools/rules"
)

func TestLoadApprovalRules(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("SUGA_HOME", home)
	userFile := filepath.Join(home, "approval-rules.yaml")
	projectFile := filepath.Join(workspace, ".suga", "approval-rules.yaml")
	assert.NoErr(t, os.MkdirAll(filepath.Dir(projectFile), 0700))
	userText := "# keep this comment\nrules:\n  - tool: shell_exec\n    command: [go test *, go build *]\n    action: allow\n"
	projectText := "rules:\n  - tool: file_write\n    path: docs/**\n    modes: [chat, run]\n    action: deny\n"
	assert.NoErr(t, os.WriteFile(userFile, []byte(userText), 0600))
	assert.NoErr(t, os.WriteFile(projectFile, []byte(projectText), 0600))

	got, err := LoadApprovalRules(workspace)
	assert.NoErr(t, err)
	assert.True(t, got.Settings.Enabled)
	assert.Eq(t, userText, string(got.User.Content))
	assert.Eq(t, projectText, string(got.Project.Content))
	assert.True(t, got.User.AST != nil)
	assert.True(t, got.Project.AST != nil)
	assert.Len(t, got.User.Rules, 1)
	assert.Eq(t, rules.SourceUser, got.User.Rules[0].Source)
	assert.Eq(t, 1, got.User.Rules[0].Index)
	assert.Eq(t, []string{"go test *", "go build *"}, got.User.Rules[0].Command)
	assert.Len(t, got.Project.Rules, 1)
	assert.Eq(t, filepath.Clean(workspace), got.Project.Rules[0].Root)
	assert.Eq(t, rules.Deny, got.Project.Rules[0].Action)
}

func TestLoadApprovalRulesDisabledKeepsLegacyPath(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("SUGA_HOME", home)
	assert.NoErr(t, os.MkdirAll(filepath.Join(workspace, ".suga"), 0700))
	assert.NoErr(t, os.WriteFile(filepath.Join(home, "approval-rules.yaml"), []byte("not: [valid"), 0600))
	assert.NoErr(t, os.WriteFile(filepath.Join(workspace, ".suga", "approval-rules.yaml"), []byte("&bad"), 0600))
	assert.NoErr(t, gkconfig.Set("tools.approval_rules", map[string]any{"enabled": false}))

	got, err := LoadApprovalRules(workspace)
	assert.NoErr(t, err)
	assert.False(t, got.Settings.Enabled)
	assert.Len(t, got.User.Rules, 0)
	assert.Len(t, got.Project.Rules, 0)
}

func TestParseApprovalRuleFileRejectsInvalidSchema(t *testing.T) {
	valid := "rules:\n  - tool: shell_exec\n    command: go test *\n    action: allow\n"
	for _, tc := range []struct {
		name string
		text string
		want string
	}{
		{name: "anchor", text: "defaults: &base\n  action: allow\nrules:\n  - tool: shell_exec\n    <<: *base\n", want: "anchors"},
		{name: "alias", text: "rules:\n  - &rule\n    tool: shell_exec\n    action: allow\n  - *rule\n", want: "anchors"},
		{name: "unknown top-level field", text: "rules: []\nother: true\n", want: "unknown field"},
		{name: "unknown rule field", text: "rules:\n  - tool: shell_exec\n    shell: sh\n    action: allow\n", want: "unknown field"},
		{name: "command and path", text: "rules:\n  - tool: shell_exec\n    command: go test\n    path: docs/**\n    action: allow\n", want: "mutually exclusive"},
		{name: "command wrong tool", text: "rules:\n  - tool: file_write\n    command: go test\n    action: allow\n", want: "command rules"},
		{name: "path glob tool", text: "rules:\n  - tool: file_*\n    path: docs/**\n    action: allow\n", want: "exact"},
		{name: "invalid action", text: "rules:\n  - tool: shell_exec\n    action: maybe\n", want: "allow, ask, or deny"},
		{name: "modes scalar", text: "rules:\n  - tool: shell_exec\n    modes: chat\n    action: allow\n", want: "modes must be a list"},
		{name: "non mapping rule", text: "rules:\n  - shell_exec\n", want: "mapping"},
		{name: "missing action", text: "rules:\n  - tool: shell_exec\n", want: "action is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseApprovalRuleFile([]byte(tc.text), "rules.yaml", rules.SourceUser, t.TempDir())
			assert.Err(t, err)
			assert.StrContains(t, err.Error(), tc.want)
		})
	}

	parsed, err := parseApprovalRuleFile([]byte(valid), "rules.yaml", rules.SourceUser, t.TempDir())
	assert.NoErr(t, err)
	assert.Len(t, parsed.Rules, 1)
}

func TestParseApprovalRuleFileLimitsSizeAndCount(t *testing.T) {
	_, err := parseApprovalRuleFile([]byte(strings.Repeat("x", maxApprovalRulesFileBytes+1)), "large.yaml", rules.SourceUser, t.TempDir())
	assert.Err(t, err)
	assert.StrContains(t, err.Error(), "exceeds")

	var text strings.Builder
	text.WriteString("rules:\n")
	for i := 0; i < maxApprovalRules+1; i++ {
		text.WriteString("  - tool: shell_exec\n    action: deny\n")
	}
	_, err = parseApprovalRuleFile([]byte(text.String()), "many.yaml", rules.SourceUser, t.TempDir())
	assert.Err(t, err)
	assert.StrContains(t, err.Error(), "more than")
}
