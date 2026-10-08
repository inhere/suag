package personal

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
)

func TestReportUsage(t *testing.T) {
	t.Run("provider input includes cache and is split out", func(t *testing.T) {
		got := ReportUsage(agentcore.Usage{InputTokens: 14, OutputTokens: 6, TotalTokens: 20, CacheReadTokens: 4, CostUSD: 0.5})
		assert.Eq(t, ReportedUsage{InputTokens: 10, OutputTokens: 6, CacheReadTokens: 4, TotalTokens: 20, CostUSD: 0.5}, got)
	})
	t.Run("cache read and write both subtracted", func(t *testing.T) {
		got := ReportUsage(agentcore.Usage{InputTokens: 100, OutputTokens: 5, CacheReadTokens: 60, CacheWriteTokens: 30})
		assert.Eq(t, 10, got.InputTokens)
		assert.Eq(t, 105, got.TotalTokens)
	})
	t.Run("never negative", func(t *testing.T) {
		got := ReportUsage(agentcore.Usage{InputTokens: 1, CacheReadTokens: 5})
		assert.Eq(t, 0, got.InputTokens)
	})
	t.Run("json omits empty cache fields", func(t *testing.T) {
		data, err := json.Marshal(ReportUsage(agentcore.Usage{InputTokens: 4, OutputTokens: 2, TotalTokens: 6}))
		assert.NoErr(t, err)
		assert.Eq(t, `{"input_tokens":4,"output_tokens":2,"total_tokens":6}`, string(data))
	})
}

func TestUsageFromSum(t *testing.T) {
	got := usageFromSum(store.Usage{InputTokens: 14, OutputTokens: 6, CacheReadTokens: 4, CacheWriteTokens: 2, CostUSD: 0.5})
	assert.Eq(t, agentcore.Usage{InputTokens: 14, OutputTokens: 6, TotalTokens: 20, CacheReadTokens: 4, CacheWriteTokens: 2, CostUSD: 0.5}, got)
	// The reported shape subtracts cache from input, same as a single turn.
	assert.Eq(t, 8, ReportUsage(got).InputTokens)
}

type usageProvider struct{ calls int }

func (*usageProvider) Name() string { return "usage-test" }

func (p *usageProvider) Generate(context.Context, *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	p.calls++
	return &agentcore.GenerateResult{
		Content: "ok", FinishReason: agentcore.FinishFinal,
		Usage: agentcore.Usage{InputTokens: 14, OutputTokens: 6, TotalTokens: 20, CacheReadTokens: 4, CostUSD: 0.5},
	}, nil
}

// Session-cumulative usage must survive a runner (process) restart.
func TestRunnerPersistsCumulativeUsageAcrossRestart(t *testing.T) {
	ctx := context.Background()
	repo, err := store.Open(newStorePath(t))
	assert.NoErr(t, err)
	t.Cleanup(func() { _ = repo.Close() })
	newRunner := func() *Runner {
		runner, err := New(Config{Repository: repo, Provider: &usageProvider{}, LockRoot: t.TempDir()})
		assert.NoErr(t, err)
		return runner
	}
	first, err := newRunner().Run(ctx, RunRequest{SessionID: "usage-session", CreateIfMissing: true, Prompt: "one"})
	assert.NoErr(t, err)
	assert.Eq(t, 20, first.Usage.TotalTokens)
	assert.Eq(t, 20, first.SessionUsage.TotalTokens)

	// A brand new Runner stands in for a restarted process.
	second, err := newRunner().Run(ctx, RunRequest{SessionID: "usage-session", Prompt: "two"})
	assert.NoErr(t, err)
	assert.Eq(t, 20, second.Usage.TotalTokens)
	assert.Eq(t, agentcore.Usage{InputTokens: 28, OutputTokens: 12, TotalTokens: 40, CacheReadTokens: 8, CostUSD: 1}, second.SessionUsage)

	// The session total is derived from model events, not from session metadata.
	sum, err := repo.SumUsage(ctx, "usage-session")
	assert.NoErr(t, err)
	assert.Eq(t, second.SessionUsage, usageFromSum(sum))
	stored, err := repo.GetSession(ctx, "usage-session")
	assert.NoErr(t, err)
	_, hasMeta := stored.Meta["usage"]
	assert.False(t, hasMeta)
}

func TestRunnerSessionUsageCountsCancelledTurn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repo, err := store.Open(newStorePath(t))
	assert.NoErr(t, err)
	t.Cleanup(func() { _ = repo.Close() })
	runner, err := New(Config{Repository: repo, Provider: &usageProvider{},
		Hooks: []agentcore.Hook{cancelAfterModel{cancel: cancel}}})
	assert.NoErr(t, err)
	result, _ := runner.Run(ctx, RunRequest{SessionID: "cancel-usage", CreateIfMissing: true, Prompt: "x"})
	assert.Eq(t, 20, result.SessionUsage.TotalTokens)
}

type cancelAfterModel struct {
	agentcore.NopHook
	cancel context.CancelFunc
}

func (h cancelAfterModel) AfterModel(context.Context, *agentcore.ModelEvent, *agentcore.Control) error {
	h.cancel()
	return nil
}
