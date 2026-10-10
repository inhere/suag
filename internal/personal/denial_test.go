package personal

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/pkg/agentcore"
)

// approvalRunner wires observe(in New) -> approval(approver) -> registry with
// a native "act" tool and an MCP "mcp_write" tool; ran counts executions.
func approvalRunner(t *testing.T, repo *store.Store, prov agentcore.Provider, approver tools.Approver, ran *int) *Runner {
	t.Helper()
	reg := tools.NewRegistry()
	reg.Register(fnTool{name: "act", fn: func() (*agentcore.ToolResult, error) {
		*ran++
		return &agentcore.ToolResult{Content: "acted"}, nil
	}})
	reg.Register(vMCP{name: "mcp_write", server: "srv"})
	runner, err := New(Config{Repository: repo, Provider: prov, Tools: tools.NewApproval(reg, approver)})
	assert.Require(t, assert.NoErr(t, err))
	return runner
}

// denyMCP refuses every MCP tool, like the CLI allowlist without entries.
var denyMCP = tools.ApproverFunc(func(_ context.Context, call agentcore.ToolCall, tool tools.Tool) error {
	if tool.Kind() == tools.KindMCP {
		return tools.Denied("需要许可：" + call.Name)
	}
	return nil
})

func TestRunnerDenialSemantics(t *testing.T) {
	ctx := context.Background()

	t.Run("a refusal is a tool error and the turn completes", func(t *testing.T) {
		repo := openObserveRepo(t)
		prov := &reqProv{steps: []*agentcore.GenerateResult{
			callStep("c1", "mcp_write", nil),
			{Content: "worked around", FinishReason: agentcore.FinishFinal},
		}}
		ran := 0
		res, err := approvalRunner(t, repo, prov, denyMCP, &ran).Run(ctx, RunRequest{Prompt: "go"})
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, RunCompleted, res.State)
		assert.Eq(t, "worked around", res.Output)
		assert.Eq(t, []string{"mcp_write"}, res.DeniedTools)
		assert.Eq(t, false, res.DenialLimitReached)
		assert.Eq(t, 2, len(prov.reqs))
		// observe recorded exactly one denial event, and the turn event counts it.
		denials := eventsOf(t, repo, res.SessionID, store.EventDenial)
		assert.Require(t, assert.Eq(t, 1, len(denials)))
		assert.Eq(t, "mcp_write", denials[0].Name)
		turns := eventsOf(t, repo, res.SessionID, store.EventTurn)
		assert.Require(t, assert.Eq(t, 1, len(turns)))
		assert.Eq(t, float64(1), turns[0].Attrs["denials"])
	})

	t.Run("the third refusal ends the turn before the next model call", func(t *testing.T) {
		repo := openObserveRepo(t)
		prov := &reqProv{steps: []*agentcore.GenerateResult{
			callStep("c1", "mcp_write", nil),
			callStep("c2", "act", nil),
			callStep("c3", "mcp_write", nil),
			callStep("c4", "mcp_write", nil),
			{Content: "never reached", FinishReason: agentcore.FinishFinal},
		}}
		ran := 0
		res, err := approvalRunner(t, repo, prov, denyMCP, &ran).Run(ctx, RunRequest{Prompt: "go"})
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, true, res.DenialLimitReached)
		assert.Eq(t, RunCompleted, res.State)
		assert.StrContains(t, res.Output, "3 次工具调用被拒绝")
		assert.Eq(t, []string{"mcp_write"}, res.DeniedTools) // distinct names
		assert.Eq(t, 4, len(prov.reqs))                      // no 5th model call
		assert.Eq(t, 1, ran)                                 // the allowed call still ran
		assert.Eq(t, 3, len(eventsOf(t, repo, res.SessionID, store.EventDenial)))
	})

	t.Run("the count is per turn", func(t *testing.T) {
		repo := openObserveRepo(t)
		prov := &reqProv{steps: []*agentcore.GenerateResult{
			callStep("c1", "mcp_write", nil),
			callStep("c2", "mcp_write", nil),
			{Content: "first", FinishReason: agentcore.FinishFinal},
			callStep("c3", "mcp_write", nil),
			{Content: "second", FinishReason: agentcore.FinishFinal},
		}}
		ran := 0
		runner := approvalRunner(t, repo, prov, denyMCP, &ran)
		first, err := runner.Run(ctx, RunRequest{Prompt: "one"})
		assert.Require(t, assert.NoErr(t, err))
		second, err := runner.Run(ctx, RunRequest{SessionID: first.SessionID, Prompt: "two"})
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, false, second.DenialLimitReached)
		assert.Eq(t, "second", second.Output)
	})

	t.Run("a cancelled approval aborts the turn", func(t *testing.T) {
		repo := openObserveRepo(t)
		prov := &reqProv{steps: []*agentcore.GenerateResult{
			callStep("c1", "mcp_write", nil),
			{Content: "must not run", FinishReason: agentcore.FinishFinal},
		}}
		cancelled := tools.ApproverFunc(func(context.Context, agentcore.ToolCall, tools.Tool) error { return context.Canceled })
		ran := 0
		res, err := approvalRunner(t, repo, prov, cancelled, &ran).Run(ctx, RunRequest{Prompt: "go"})
		assert.Eq(t, true, errors.Is(err, context.Canceled))
		assert.Eq(t, true, errors.Is(err, tools.ErrAborted))
		assert.Eq(t, RunCanceled, res.State)
		assert.Eq(t, 1, len(prov.reqs)) // never fed back to the model
		assert.Eq(t, 0, len(eventsOf(t, repo, res.SessionID, store.EventDenial)))
	})

	t.Run("a tool's own timeout stays a tool error", func(t *testing.T) {
		repo := openObserveRepo(t)
		reg := tools.NewRegistry()
		reg.Register(fnTool{name: "slow", fn: func() (*agentcore.ToolResult, error) { return nil, context.DeadlineExceeded }})
		prov := &reqProv{steps: []*agentcore.GenerateResult{
			callStep("c1", "slow", nil),
			{Content: "recovered", FinishReason: agentcore.FinishFinal},
		}}
		runner, err := New(Config{Repository: repo, Provider: prov, Tools: tools.NewApproval(reg, denyMCP)})
		assert.Require(t, assert.NoErr(t, err))
		res, err := runner.Run(ctx, RunRequest{Prompt: "go"})
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "recovered", res.Output)
		assert.Eq(t, 0, len(res.DeniedTools))
	})
}

// TestRunnerDisabledAndUnknownTools: a known-but-disabled tool is refused with
// its enable hint and counts; an unknown name is a plain error that does not.
func TestRunnerDisabledAndUnknownTools(t *testing.T) {
	ctx := context.Background()
	newRunner := func(t *testing.T, repo *store.Store, prov agentcore.Provider) *Runner {
		reg := tools.NewRegistry()
		reg.Register(fnTool{name: "act", fn: func() (*agentcore.ToolResult, error) {
			return &agentcore.ToolResult{Content: "acted"}, nil
		}})
		approval := tools.NewApproval(reg, tools.ApproverFunc(func(context.Context, agentcore.ToolCall, tools.Tool) error { return nil }))
		approval.SetDisabled(map[string]string{"shell_exec": "加 --allow-write"})
		runner, err := New(Config{Repository: repo, Provider: prov, Tools: approval})
		assert.Require(t, assert.NoErr(t, err))
		return runner
	}

	t.Run("three disabled calls end the turn", func(t *testing.T) {
		repo := openObserveRepo(t)
		prov := &reqProv{steps: []*agentcore.GenerateResult{
			callStep("c1", "shell_exec", nil),
			callStep("c2", "shell_exec", nil),
			callStep("c3", "shell_exec", nil),
			{Content: "never reached", FinishReason: agentcore.FinishFinal},
		}}
		res, err := newRunner(t, repo, prov).Run(ctx, RunRequest{Prompt: "go"})
		assert.Require(t, assert.NoErr(t, err))
		assert.True(t, res.DenialLimitReached)
		assert.Eq(t, 3, len(prov.reqs))
		assert.StrContains(t, toolResultText(prov.reqs[1]), "加 --allow-write")
	})
	t.Run("unknown names do not count", func(t *testing.T) {
		repo := openObserveRepo(t)
		prov := &reqProv{steps: []*agentcore.GenerateResult{
			callStep("c1", "bash", nil),
			callStep("c2", "bash", nil),
			callStep("c3", "bash", nil),
			{Content: "done", FinishReason: agentcore.FinishFinal},
		}}
		res, err := newRunner(t, repo, prov).Run(ctx, RunRequest{Prompt: "go"})
		assert.Require(t, assert.NoErr(t, err))
		assert.False(t, res.DenialLimitReached)
		assert.Eq(t, "done", res.Output)
		assert.StrContains(t, toolResultText(prov.reqs[1]), "available tools: act")
	})
}

// toolResultText joins the tool result contents (and tool error texts) a
// request carries.
func toolResultText(req *agentcore.GenerateRequest) string {
	var sb strings.Builder
	for _, m := range req.Messages {
		if m.ToolResult != nil {
			sb.WriteString(m.ToolResult.Content + "\n")
		}
		if m.Role == "tool" {
			sb.WriteString(m.Content + "\n")
		}
	}
	return sb.String()
}
