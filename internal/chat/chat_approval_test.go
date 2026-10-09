package chat

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/pkg/agentcore"
	agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"
	"github.com/inhere/suag/pkg/agentcore/mock"
)

// shellStub stands in for shell_exec and counts its runs.
type shellStub struct{ runs int }

func (t *shellStub) Name() string        { return "shell_exec" }
func (t *shellStub) Description() string { return "shell stub" }
func (t *shellStub) Kind() tools.Kind    { return tools.KindNative }
func (t *shellStub) Schema() agentcoreapi.ToolSchema {
	return agentcoreapi.ToolSchema{Name: "shell_exec"}
}
func (t *shellStub) Execute(_ context.Context, call agentcoreapi.ToolCall) (*agentcoreapi.ToolResult, error) {
	t.runs++
	return &agentcoreapi.ToolResult{CallID: call.CallID, Name: call.Name, Content: "ran"}, nil
}

// askerFunc adapts a function to tools.Asker.
type askerFunc func(ctx context.Context, q tools.Question) (string, error)

func (f askerFunc) Ask(ctx context.Context, q tools.Question) (string, error) { return f(ctx, q) }

// newApprovalChatRunner builds a runner whose shell_exec goes through an
// approval layer that asks asker (as the chat approver does).
func newApprovalChatRunner(t *testing.T, asker tools.Asker, steps ...mock.Step) (*personal.Runner, *store.Store, *shellStub) {
	t.Helper()
	repo, err := store.Open(newTestStorePath(t))
	assert.Require(t, assert.NoErr(t, err))
	t.Cleanup(func() { _ = repo.Close() })
	shell := &shellStub{}
	registry := tools.NewRegistry()
	registry.Register(shell)
	approver := tools.ApproverFunc(func(ctx context.Context, call agentcore.ToolCall, _ tools.Tool) error {
		answer, err := asker.Ask(ctx, tools.Question{Tool: call.Name, Prompt: "?"})
		if err != nil {
			return err
		}
		if answer == "y" {
			return nil
		}
		return tools.Denied("用户拒绝执行 " + call.Name)
	})
	runner, err := personal.New(personal.Config{
		Repository: repo, Provider: mock.NewProvider(steps...),
		Tools: tools.NewApproval(registry, approver),
	})
	assert.Require(t, assert.NoErr(t, err))
	return runner, repo, shell
}

func TestChatApprovalOutcomes(t *testing.T) {
	shellCall := func(id string) mock.Step {
		return mock.ToolCall(id, "shell_exec", map[string]any{"command": "echo hi"})
	}

	t.Run("cancelled question ends only the turn", func(t *testing.T) {
		bound := tools.NewBindableAsker()
		// stdin is not a terminal, so Run leaves this test binding in place.
		bound.Bind(askerFunc(func(context.Context, tools.Question) (string, error) { return "", errAskCancelled }))
		runner, repo, shell := newApprovalChatRunner(t, bound, shellCall("c1"), mock.Final("second turn answer"))
		var stdout, stderr bytes.Buffer
		err := Run(context.Background(), runner, Options{Asker: bound},
			strings.NewReader("run it\nnext\n:quit\n"), &stdout, &stderr, nil)
		assert.NoErr(t, err)
		assert.StrContains(t, stdout.String(), "已取消")
		assert.NotContains(t, stderr.String(), "Error:")
		assert.StrContains(t, stdout.String(), "second turn answer") // chat went on
		assert.Eq(t, 0, shell.runs)

		sessions, err := repo.ListSessions(context.Background())
		assert.NoErr(t, err)
		cancelled, err := repo.ListTurns(context.Background(), sessions[0].ID, store.TurnCancelled)
		assert.NoErr(t, err)
		assert.Len(t, cancelled, 1)
	})

	t.Run("refused tools are listed at the end of the turn", func(t *testing.T) {
		asker := askerFunc(func(context.Context, tools.Question) (string, error) { return "n", nil })
		runner, _, shell := newApprovalChatRunner(t, asker, shellCall("c1"), mock.Final("worked around it"))
		var stdout, stderr bytes.Buffer
		err := Run(context.Background(), runner, Options{}, strings.NewReader("run it\n:quit\n"), &stdout, &stderr, nil)
		assert.NoErr(t, err)
		assert.StrContains(t, stdout.String(), "error: 用户拒绝执行 shell_exec")
		assert.StrContains(t, stdout.String(), "本轮被拒绝的工具：shell_exec")
		assert.NotContains(t, stdout.String(), "Input required to authorize an MCP tool.")
		assert.Eq(t, 0, shell.runs)
	})

	t.Run("three refusals end the turn", func(t *testing.T) {
		asks := 0
		asker := askerFunc(func(context.Context, tools.Question) (string, error) { asks++; return "", nil })
		runner, _, _ := newApprovalChatRunner(t, asker, shellCall("c1"), shellCall("c2"), shellCall("c3"), shellCall("c4"))
		var stdout, stderr bytes.Buffer
		err := Run(context.Background(), runner, Options{}, strings.NewReader("run it\n:quit\n"), &stdout, &stderr, nil)
		assert.NoErr(t, err)
		assert.Eq(t, 3, asks)
		assert.StrContains(t, stdout.String(), "3 次工具调用被拒绝")
		assert.StrContains(t, stdout.String(), "本轮被拒绝的工具：shell_exec")
	})

	t.Run("allowed tool runs", func(t *testing.T) {
		asker := askerFunc(func(context.Context, tools.Question) (string, error) { return "y", nil })
		runner, _, shell := newApprovalChatRunner(t, asker, shellCall("c1"), mock.Final("done"))
		var stdout, stderr bytes.Buffer
		err := Run(context.Background(), runner, Options{}, strings.NewReader("run it\n:quit\n"), &stdout, &stderr, nil)
		assert.NoErr(t, err)
		assert.Eq(t, 1, shell.runs)
		assert.NotContains(t, stdout.String(), "本轮被拒绝的工具")
	})
}

func TestRendererApprovalQuestion(t *testing.T) {
	r := newRenderer(rendererOptions{Out: &bytes.Buffer{}})
	q := tools.Question{Tool: "shell_exec", Details: []string{"命令：echo a\necho b", "x"}, Prompt: "允许执行 shell_exec？"}
	assert.Eq(t, "  命令：echo a\n    echo b\n  x\n", r.ApprovalDetails(q))
	assert.Eq(t, "允许执行 shell_exec？", r.ApprovalPrompt(q))
}
