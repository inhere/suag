package wiring

import (
	"context"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/tools"
	approvalrules "github.com/inhere/suga/pkg/agentcore/tools/rules"
)

func TestChatScopedApprovalPromptsAndSessionOnlyPersistence(t *testing.T) {
	workspace := t.TempDir()
	policy := approvalPolicy{
		Mode: approvalrules.ModeChat, Workspace: workspace, AlwaysPrefixTokens: 2, PersistAsk: true,
		Defaults: defaultApprovalDefaults(approvalrules.ModeChat, false),
	}
	asker := &scriptedAsker{answers: []string{"a", "u"}}
	approver := newChatApprover(chatApproverOptions{gate: newCLIMCPGateWithPolicy(policy), asker: asker, workspace: workspace})
	tool := &nativeStubTool{name: "shell_exec"}
	assert.NoErr(t, approver.Decide(context.Background(), agentcore.ToolCall{Name: "shell_exec", Arguments: map[string]any{"command": "go test ./..."}}, tool))
	assert.Eq(t, 2, len(asker.questions))
	assert.StrContains(t, asker.questions[0].Prompt, `[a]本会话总是允许“go test”开头的命令`)
	assert.StrContains(t, asker.questions[1].Prompt, "写回尚未启用")
	assert.StrContains(t, asker.questions[1].Prompt, "[u]")
	assert.Len(t, approver.policy.Rules, 1)
	assert.False(t, tools.IsMCP(tool))
}
