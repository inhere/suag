package personal

import (
	"context"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore"
)

func TestBudgetSnapshotUsesConfiguredScopeAndCacheRule(t *testing.T) {
	for _, tc := range []struct {
		name   string
		budget BudgetSettings
		base   agentcore.Usage
		run    agentcore.Usage
		held   agentcore.Usage
		want   BudgetMetric
		exceed bool
	}{
		{name: "turn excludes held and cache read", budget: BudgetSettings{Tokens: 10, Scope: "turn"}, run: agentcore.Usage{InputTokens: 12, CacheReadTokens: 9, OutputTokens: 2}, held: agentcore.Usage{InputTokens: 4, OutputTokens: 1}, want: BudgetMetric{Limit: 10, Used: 1, Remaining: 9}},
		{name: "session includes base and run", budget: BudgetSettings{Tokens: 10, Scope: "session"}, base: agentcore.Usage{InputTokens: 7}, run: agentcore.Usage{InputTokens: 2, OutputTokens: 2}, want: BudgetMetric{Limit: 10, Used: 11, Remaining: 0}, exceed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			meter := &runUsageMeter{budget: tc.budget, base: tc.base, run: RunUsage{Usage: tc.run}, held: tc.held}
			got := meter.budgetSnapshot()
			assert.Eq(t, tc.want, got.Tokens)
			assert.Eq(t, tc.exceed, got.Exceeded)
		})
	}
}

func TestBudgetHookRequestsWrapUpOnceAndRecordsEvent(t *testing.T) {
	repo := openObserveRepo(t)
	provider := &usageSteps{steps: []func() (*agentcore.GenerateResult, error){
		withUsage(callStep("c1", "unknown", nil), 10, 1),
		withUsage(&agentcore.GenerateResult{Content: "done", FinishReason: agentcore.FinishFinal}, 1, 1),
	}}
	var updates []UsageUpdate
	runner, err := New(Config{
		Repository: repo, Provider: provider, Budget: BudgetSettings{Tokens: 5, Scope: "turn"},
	})
	assert.Require(t, assert.NoErr(t, err))
	result, err := runner.Run(context.Background(), RunRequest{Prompt: "inspect", Emit: func(_ context.Context, event Event) error {
		if event.Kind == "usage" && event.Usage != nil {
			updates = append(updates, *event.Usage)
		}
		return nil
	}})
	assert.Require(t, assert.NoErr(t, err))
	assert.Require(t, assert.Len(t, updates, 2))
	assert.Require(t, assert.NotNil(t, updates[0].Budget))
	assert.True(t, updates[0].Budget.Exceeded)
	assert.Eq(t, "max_tokens", result.WrapUp)
	assert.Eq(t, 2, provider.n)

	events := eventsOf(t, repo, result.SessionID, store.EventBudget)
	assert.Require(t, assert.Len(t, events, 1))
	assert.Eq(t, "exceeded", events[0].Name)
	assert.Eq(t, "turn", events[0].Attrs["scope"])
	assert.Eq(t, float64(5), events[0].Attrs["limit_tokens"])
	assert.Eq(t, float64(11), events[0].Attrs["used_tokens"])
	assert.Eq(t, "max_tokens", events[0].Attrs["reason"])
}

func TestBudgetFinalUsageCarriesWrapUpReason(t *testing.T) {
	repo := openObserveRepo(t)
	provider := &usageSteps{steps: []func() (*agentcore.GenerateResult, error){
		withUsage(&agentcore.GenerateResult{Content: "done", FinishReason: agentcore.FinishFinal}, 10, 1),
	}}
	observed := &usageEvents{}
	runner, err := New(Config{Repository: repo, Provider: provider,
		Budget: BudgetSettings{Tokens: 5, Scope: "turn"}})
	assert.Require(t, assert.NoErr(t, err))
	result, err := runner.Run(context.Background(), RunRequest{Prompt: "inspect", Emit: observed.emit})
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, "max_tokens", result.WrapUp)
	assert.Require(t, assert.NotNil(t, observed.final))
	assert.Require(t, assert.NotNil(t, observed.final.Budget))
	assert.Eq(t, "max_tokens", observed.final.Budget.Reason)
}

func TestSessionBudgetShortCircuitsBeforeModelWithoutAssistant(t *testing.T) {
	ctx := context.Background()
	repo := openObserveRepo(t)
	provider := &usageSteps{steps: []func() (*agentcore.GenerateResult, error){
		withUsage(&agentcore.GenerateResult{Content: "first answer", FinishReason: agentcore.FinishFinal}, 10, 1),
	}}
	runner, err := New(Config{Repository: repo, Provider: provider,
		Budget: BudgetSettings{Tokens: 5, Scope: "session"}})
	assert.Require(t, assert.NoErr(t, err))

	first, err := runner.Run(ctx, RunRequest{Prompt: "first"})
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, 1, provider.n)
	assert.Eq(t, 11, first.SessionUsage.InputTokens+first.SessionUsage.OutputTokens)

	second, err := runner.Run(ctx, RunRequest{SessionID: first.SessionID, Prompt: "second"})
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, 1, provider.n)
	assert.Eq(t, RunCompleted, second.State)
	assert.Eq(t, "max_tokens", second.WrapUp)
	assert.Eq(t, "", second.Output)

	messages, err := repo.AgentStorage().LoadMessages(ctx, second.TurnID)
	assert.Require(t, assert.NoErr(t, err))
	assert.Require(t, assert.Len(t, messages, 1))
	assert.Eq(t, "user", messages[0].Role)
	assert.Eq(t, "second", messages[0].Content)

	budgetEvents := eventsOf(t, repo, first.SessionID, store.EventBudget)
	assert.Require(t, assert.Len(t, budgetEvents, 2))
	assert.Eq(t, "max_tokens", budgetEvents[1].Attrs["reason"])
	assert.Eq(t, float64(11), budgetEvents[1].Attrs["used_tokens"])

	// Reopening without the budget proves the short-circuited turn left a legal
	// history: the next request has no adjacent assistant messages.
	var next nextProvider
	runAgain, err := New(Config{Repository: repo, Provider: &next})
	assert.Require(t, assert.NoErr(t, err))
	_, err = runAgain.Run(ctx, RunRequest{SessionID: first.SessionID, Prompt: "third"})
	assert.Require(t, assert.NoErr(t, err))
	assert.Require(t, assert.Len(t, next.requests, 1))
	for i := 1; i < len(next.requests[0].Messages); i++ {
		assert.False(t, next.requests[0].Messages[i-1].Role == "assistant" && next.requests[0].Messages[i].Role == "assistant")
	}
}

type nextProvider struct{ requests []*agentcore.GenerateRequest }

func (*nextProvider) Name() string { return "next-test" }

func (p *nextProvider) Generate(_ context.Context, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	p.requests = append(p.requests, req)
	return &agentcore.GenerateResult{Content: "third answer", FinishReason: agentcore.FinishFinal}, nil
}

func TestBudgetCostUsesProviderOrConfiguredPrice(t *testing.T) {
	t.Run("provider cost takes precedence", func(t *testing.T) {
		meter := &runUsageMeter{budget: BudgetSettings{CostUSD: 1, Scope: "turn"}, run: RunUsage{Usage: agentcore.Usage{CostUSD: 1.2}}}
		hook := &budgetHook{settings: meter.budget, meter: meter}
		ctrl := agentcore.NewControl()
		assert.NoErr(t, hook.AfterModel(context.Background(), nil, ctrl))
		assert.Eq(t, "max_cost_usd", ctrl.WrapUpReason)
	})

	t.Run("configured price is already on the meter", func(t *testing.T) {
		meter := &runUsageMeter{budget: BudgetSettings{CostUSD: 1, Scope: "turn"}, run: RunUsage{Usage: agentcore.Usage{InputTokens: 1_000_000, CostUSD: 1.5}}}
		got := meter.budgetSnapshot()
		assert.Eq(t, 1.5, got.CostUSD.Used)
		assert.True(t, got.Exceeded)
	})
}

func TestBudgetWarningIsReportedOnce(t *testing.T) {
	meter := &runUsageMeter{budget: BudgetSettings{Tokens: 10, CostUSD: 1, Scope: "turn", Estimate: "off"}}
	var warnings []string
	hook := &budgetHook{settings: meter.budget, meter: meter, onWarning: func(_ context.Context, message string) { warnings = append(warnings, message) }}
	ctrl := agentcore.NewControl()
	result := &agentcore.ModelEvent{Result: &agentcore.GenerateResult{Usage: agentcore.Usage{}}}
	assert.NoErr(t, hook.AfterModel(context.Background(), result, ctrl))
	assert.NoErr(t, hook.AfterModel(context.Background(), result, ctrl))
	assert.Eq(t, 2, len(warnings))
	assert.StrContains(t, warnings[0], "成本预算")
	assert.StrContains(t, warnings[1], "关闭估算")
}
