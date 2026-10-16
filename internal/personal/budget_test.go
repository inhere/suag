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
