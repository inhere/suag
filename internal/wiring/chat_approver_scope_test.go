package wiring

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/tools"
	approvalrules "github.com/inhere/suga/pkg/agentcore/tools/rules"
)

func TestChatScopedApprovalPromptsAndPersistence(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	t.Setenv("SUGA_HOME", home)
	policy := approvalPolicy{
		Mode: approvalrules.ModeChat, Workspace: workspace, AlwaysPrefixTokens: 2, PersistAsk: true,
		Defaults: defaultApprovalDefaults(approvalrules.ModeChat, false),
	}
	asker := &scriptedAsker{answers: []string{"a", "u"}}
	var events []ApprovalRulePersistEvent
	approver := newChatApprover(chatApproverOptions{
		gate: newCLIMCPGateWithPolicy(policy), asker: asker, workspace: workspace,
		persistReporter: func(_ context.Context, event ApprovalRulePersistEvent) { events = append(events, event) },
	})
	tool := &nativeStubTool{name: "shell_exec"}
	assert.NoErr(t, approver.Decide(context.Background(), agentcore.ToolCall{Name: "shell_exec", Arguments: map[string]any{"command": "go test ./..."}}, tool))
	assert.Eq(t, 2, len(asker.questions))
	assert.StrContains(t, asker.questions[0].Prompt, `[a]本会话总是允许“go test”开头的命令`)
	assert.StrContains(t, asker.questions[1].Prompt, "把这条规则写进规则文件")
	assert.StrContains(t, asker.questions[1].Prompt, "[u]")
	assert.Len(t, approver.policy.Rules, 1)
	assert.Len(t, events, 1)
	assert.Eq(t, approvalrules.Allow, events[0].Action)
	assert.Eq(t, "ok", events[0].Result)
	assert.False(t, tools.IsMCP(tool))

	path := filepath.Join(home, "approval-rules.yaml")
	text, err := os.ReadFile(path)
	assert.Require(t, assert.NoErr(t, err))
	parsed, err := parseApprovalRuleFile(text, path, approvalrules.SourceUser, workspace)
	assert.Require(t, assert.NoErr(t, err))
	assert.Require(t, assert.Len(t, parsed.Rules, 1))
	assert.Eq(t, []string{"go test *"}, parsed.Rules[0].Command)
	assert.Eq(t, approvalrules.Allow, parsed.Rules[0].Action)

	loaded, err := LoadApprovalRules(workspace)
	assert.Require(t, assert.NoErr(t, err))
	assert.Require(t, assert.Len(t, loaded.EffectiveRules(), 1))
	assert.Eq(t, approvalrules.Allow, loaded.EffectiveRules()[0].Action)
}

func TestChatProjectDenyPersistenceAndWriteFailureKeepSessionRule(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	t.Setenv("SUGA_HOME", home)
	projectPath := filepath.Join(workspace, ".suga", "approval-rules.yaml")

	t.Run("project deny is written", func(t *testing.T) {
		policy := approvalPolicy{Mode: approvalrules.ModeChat, Workspace: workspace, PersistAsk: true,
			Defaults: defaultApprovalDefaults(approvalrules.ModeChat, false)}
		asker := &scriptedAsker{answers: []string{"d", "p"}}
		approver := newChatApprover(chatApproverOptions{gate: newCLIMCPGateWithPolicy(policy), asker: asker, workspace: workspace})
		err := approver.Decide(context.Background(), agentcore.ToolCall{Name: "file_write", Arguments: map[string]any{"path": "docs/a.txt"}}, &nativeStubTool{name: "file_write"})
		assert.ErrIs(t, err, tools.ErrDenied)
		text, readErr := os.ReadFile(projectPath)
		assert.Require(t, assert.NoErr(t, readErr))
		parsed, parseErr := parseApprovalRuleFile(text, projectPath, approvalrules.SourceProject, workspace)
		assert.Require(t, assert.NoErr(t, parseErr))
		assert.Require(t, assert.Len(t, parsed.Rules, 1))
		assert.Eq(t, approvalrules.Deny, parsed.Rules[0].Action)
	})

	t.Run("bad file does not remove the session rule", func(t *testing.T) {
		badHome := t.TempDir()
		t.Setenv("SUGA_HOME", badHome)
		userPath := filepath.Join(badHome, "approval-rules.yaml")
		assert.NoErr(t, os.WriteFile(userPath, []byte("rules: [\n"), 0o600))
		policy := approvalPolicy{Mode: approvalrules.ModeChat, Workspace: workspace, PersistAsk: true,
			Defaults: defaultApprovalDefaults(approvalrules.ModeChat, false)}
		asker := &scriptedAsker{answers: []string{"a", "u"}}
		var events []ApprovalRulePersistEvent
		approver := newChatApprover(chatApproverOptions{gate: newCLIMCPGateWithPolicy(policy), asker: asker, workspace: workspace,
			persistReporter: func(_ context.Context, event ApprovalRulePersistEvent) { events = append(events, event) }})
		err := approver.Decide(context.Background(), agentcore.ToolCall{Name: "shell_exec", Arguments: map[string]any{"command": "go test"}}, &nativeStubTool{name: "shell_exec"})
		assert.NoErr(t, err)
		assert.Len(t, approver.policy.Rules, 1)
		assert.Len(t, events, 1)
		assert.Eq(t, "error", events[0].Result)
		assert.StrContains(t, events[0].File, "approval-rules.yaml")
	})
}
