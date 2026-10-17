package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/tools"
)

type chatEmptyFinalProvider struct{ calls int }

func (p *chatEmptyFinalProvider) Name() string { return "chat-empty-final" }

func (p *chatEmptyFinalProvider) Generate(context.Context, *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	p.calls++
	if p.calls == 1 {
		return &agentcore.GenerateResult{
			ToolCalls:    []agentcore.ToolCall{{CallID: "call-1", Name: "write"}},
			FinishReason: agentcore.FinishToolCalls,
		}, nil
	}
	return &agentcore.GenerateResult{FinishReason: agentcore.FinishFinal}, nil
}

type chatEmptyFinalTool struct{}

func (chatEmptyFinalTool) Name() string        { return "write" }
func (chatEmptyFinalTool) Description() string { return "write" }
func (chatEmptyFinalTool) Schema() agentcore.ToolSchema {
	return agentcore.ToolSchema{Name: "write", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}}}
}
func (chatEmptyFinalTool) Execute(context.Context, agentcore.ToolCall) (*agentcore.ToolResult, error) {
	return &agentcore.ToolResult{Content: "written"}, nil
}

// budgetChatProvider is a fake provider with the same per-call usage shape as
// fakellm: its response crosses the budget and the completed result is marked
// as a budget wrap-up.
type budgetChatProvider struct {
	responses []string
	calls     int
}

func (p *budgetChatProvider) Name() string { return "budget-fake" }

func (p *budgetChatProvider) Generate(context.Context, *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	text := p.responses[p.calls]
	p.calls++
	return &agentcore.GenerateResult{
		Content: text, FinishReason: agentcore.FinishFinal,
		Usage: agentcore.Usage{InputTokens: 10, OutputTokens: 2},
	}, nil
}

func TestChatBudgetWrapUpAndSessionShortCircuit(t *testing.T) {
	repo, err := store.Open(newTestStorePath(t))
	assert.Require(t, assert.NoErr(t, err))
	t.Cleanup(func() { _ = repo.Close() })
	provider := &budgetChatProvider{responses: []string{"first answer"}}
	runner, err := personal.New(personal.Config{
		Repository: repo, Provider: provider,
		Budget: personal.BudgetSettings{Tokens: 5, Scope: "session"},
	})
	assert.Require(t, assert.NoErr(t, err))

	var stdout, stderr strings.Builder
	err = Run(context.Background(), runner, Options{}, strings.NewReader("first\nsecond\n:quit\n"), &stdout, &stderr, nil)
	assert.NoErr(t, err)

	out := stdout.String()
	assert.Contains(t, out, "本轮因 token 预算用尽收尾")
	// The short-circuited second turn has no output, so it must not print an
	// empty answer marker before its zero-usage status line.
	assert.NotContains(t, out, "▣ \nin 0 / out 0 tokens")
}

func TestChatEmptyFinalAfterToolDoesNotPrintAnswerMarker(t *testing.T) {
	repo, err := store.Open(newTestStorePath(t))
	assert.Require(t, assert.NoErr(t, err))
	t.Cleanup(func() { _ = repo.Close() })
	registry := tools.NewRegistry()
	registry.MustRegister(chatEmptyFinalTool{})
	provider := &chatEmptyFinalProvider{}
	runner, err := personal.New(personal.Config{Repository: repo, Provider: provider, Tools: registry})
	assert.Require(t, assert.NoErr(t, err))

	var stdout, stderr strings.Builder
	err = Run(context.Background(), runner, Options{}, strings.NewReader("write\n:quit\n"), &stdout, &stderr, nil)
	assert.NoErr(t, err)
	assert.NotContains(t, stdout.String(), "▣ \n")
	assert.Eq(t, 4, provider.calls)
}
