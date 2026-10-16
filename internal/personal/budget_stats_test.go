package personal

import (
	"context"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal/store"
)

func TestSessionStatsAndBudgetUseTheSameModelEvents(t *testing.T) {
	repo := openObserveRepo(t)
	ctx := context.Background()
	assert.NoErr(t, repo.CreateSession(ctx, store.Session{ID: "budget-stats", Status: store.SessionOpen}))
	for _, event := range []store.Event{
		{SessionID: "budget-stats", Kind: store.EventModel, Name: "provider", InputTokens: 100, OutputTokens: 10, CacheReadTokens: 20, CacheWriteTokens: 5, Attrs: map[string]any{"model": "model", "cost_usd": 0.2}},
		{SessionID: "budget-stats", Kind: store.EventModel, Name: "provider", InputTokens: 40, OutputTokens: 4, CacheReadTokens: 10, CacheWriteTokens: 2, Attrs: map[string]any{"model": "model", "cost_usd": 0.1, "estimated": true}},
	} {
		_, err := repo.AppendEvent(ctx, event)
		assert.NoErr(t, err)
	}

	t.Run("session budget token spend matches stats model events", func(t *testing.T) {
		sum, err := repo.SumUsage(ctx, "budget-stats")
		assert.NoErr(t, err)
		report, err := repo.BuildReport(ctx, store.EventFilter{SessionID: "budget-stats", WithAgents: true})
		assert.NoErr(t, err)
		assert.Require(t, assert.Len(t, report.Models, 1))

		meter := &runUsageMeter{base: usageFromSum(sum), budget: BudgetSettings{Tokens: 1000, Scope: "session"}}
		snapshot := meter.budgetSnapshot()
		// Stats reports uncached input after removing cache read and write. Add
		// cache write back to compare it with the budget's input-cache_read rule.
		statsSpend := report.Models[0].InputTokens + report.Models[0].CacheWrite + report.Models[0].OutputTokens
		assert.Eq(t, float64(statsSpend), snapshot.Tokens.Used)
		assert.Eq(t, int(sum.Calls), report.Models[0].Calls)
		assert.Eq(t, 1, report.Models[0].EstimatedCalls)
	})
}
