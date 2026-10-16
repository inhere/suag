package personal

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/mock"
	"github.com/inhere/suga/pkg/agentcore/tools"
)

func TestApprovalRuleObservationAttrsAndTrace(t *testing.T) {
	t.Run("allow rule is recorded on the tool event", func(t *testing.T) {
		repo := openObserveRepo(t)
		reg := tools.NewRegistry()
		reg.Register(fnTool{name: "act", fn: func() (*agentcore.ToolResult, error) {
			return &agentcore.ToolResult{Content: "acted"}, nil
		}})
		approval := tools.NewApproval(reg, tools.ApproverFunc(func(ctx context.Context, _ agentcore.ToolCall, _ agentcore.Tool) error {
			tools.ReportRule(ctx, `user#3 shell_exec command="go test *" action=allow`, "")
			return nil
		}))
		runner, err := New(Config{Repository: repo, Provider: mock.NewProvider(mock.ToolCall("c1", "act", map[string]any{}), mock.Final("done")), Tools: approval})
		assert.Require(t, assert.NoErr(t, err))
		res, err := runner.Run(context.Background(), RunRequest{Prompt: "run"})
		assert.Require(t, assert.NoErr(t, err))
		events := eventsOf(t, repo, res.SessionID, store.EventTool)
		assert.Require(t, assert.Len(t, events, 1))
		assert.Eq(t, `user#3 shell_exec command="go test *" action=allow`, events[0].Attrs["approval_rule"])
		assert.NotContains(t, events[0].Attrs, "approval_scope")
		line := traceLine(events[0])
		assert.StrContains(t, line, `approval=rule(user#3: shell_exec "go test *")`)
	})

	t.Run("session scope and denial are both observable", func(t *testing.T) {
		repo := openObserveRepo(t)
		reg := tools.NewRegistry()
		reg.Register(fnTool{name: "act", fn: func() (*agentcore.ToolResult, error) {
			return nil, errors.New("must not run")
		}})
		approval := tools.NewApproval(reg, tools.ApproverFunc(func(ctx context.Context, _ agentcore.ToolCall, _ agentcore.Tool) error {
			tools.ReportApproval(ctx, tools.ApprovedAlways, 0)
			tools.ReportRule(ctx, `session#1 shell_exec command="go test *" action=allow`, "shell_exec:go test")
			return tools.Denied("blocked")
		}))
		runner, err := New(Config{Repository: repo, Provider: mock.NewProvider(mock.ToolCall("c1", "act", map[string]any{}), mock.Final("done")), Tools: approval})
		assert.Require(t, assert.NoErr(t, err))
		res, err := runner.Run(context.Background(), RunRequest{Prompt: "run"})
		assert.Require(t, assert.NoErr(t, err))
		denials := eventsOf(t, repo, res.SessionID, store.EventDenial)
		assert.Require(t, assert.Len(t, denials, 1))
		assert.Eq(t, `session#1 shell_exec command="go test *" action=allow`, denials[0].Attrs["approval_rule"])
		assert.Eq(t, "shell_exec:go test", denials[0].Attrs["approval_scope"])
		assert.StrContains(t, traceLine(denials[0]), `approval=always(shell_exec "go test")`)
	})
}

func TestApprovalRulePersistTraceLine(t *testing.T) {
	line := traceLine(store.Event{Kind: approvalRulePersistEvent, Name: "rule", Status: "ok", Attrs: map[string]any{
		"file": "~/.suga/approval-rules.yaml", "action": "allow", "result": "ok",
	}})
	assert.StrContains(t, line, "file=~/.suga/approval-rules.yaml action=allow result=ok")
	assert.True(t, strings.Contains(line, "approval_rule_persist"))
}
