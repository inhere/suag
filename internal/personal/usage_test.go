package personal

import (
	"context"
	"encoding/json"
	"errors"
	"math"
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

// Early failures (before the turn is finished) must still report the usage the
// session already accumulated, not a zero value.
func TestRunnerEarlyFailureKeepsSessionUsage(t *testing.T) {
	ctx := context.Background()
	repo, err := store.Open(newStorePath(t))
	assert.NoErr(t, err)
	t.Cleanup(func() { _ = repo.Close() })
	ruleErr := errors.New("rules unavailable")
	failRules := false
	runner, err := New(Config{Repository: repo, Provider: &usageProvider{}, LockRoot: t.TempDir(),
		RuleSource: func() (string, string, error) {
			if failRules {
				return "", "", ruleErr
			}
			return "", "h", nil
		}})
	assert.NoErr(t, err)
	first, err := runner.Run(ctx, RunRequest{SessionID: "early-fail", CreateIfMissing: true, Prompt: "one"})
	assert.NoErr(t, err)
	assert.Eq(t, 20, first.SessionUsage.TotalTokens)

	t.Run("rule source failure", func(t *testing.T) {
		failRules = true
		res, err := runner.Run(ctx, RunRequest{SessionID: "early-fail", Prompt: "two"})
		assert.True(t, errors.Is(err, ruleErr))
		assert.Eq(t, first.SessionUsage, res.SessionUsage)
	})

	t.Run("session not ready", func(t *testing.T) {
		model, err := repo.GetSession(ctx, "early-fail")
		assert.NoErr(t, err)
		model.Status = store.SessionArchived
		assert.NoErr(t, repo.UpdateSession(ctx, model))
		res, err := runner.Run(ctx, RunRequest{SessionID: "early-fail", Prompt: "three"})
		assert.Err(t, err)
		assert.Eq(t, first.SessionUsage, res.SessionUsage)
	})
}

func TestPriceCost(t *testing.T) {
	price := Price{Input: 2, Output: 10, CacheRead: 0.5, CacheWrite: 4}
	t.Run("cache hit is billed at its own rates", func(t *testing.T) {
		// Provider-normalized: input 1_000_000 includes 600_000 cache read and
		// 100_000 cache write, so plain input is 300_000.
		u := agentcore.Usage{InputTokens: 1_000_000, OutputTokens: 200_000, CacheReadTokens: 600_000, CacheWriteTokens: 100_000}
		// 0.3*2 + 0.2*10 + 0.6*0.5 + 0.1*4 = 0.6 + 2 + 0.3 + 0.4
		assert.Eq(t, 3.3, math.Round(price.Cost(u)*1e9)/1e9)
	})
	t.Run("cache larger than input floors plain input at zero", func(t *testing.T) {
		u := agentcore.Usage{InputTokens: 10, CacheReadTokens: 1_000_000}
		assert.Eq(t, 0.5, math.Round(price.Cost(u)*1e9)/1e9)
	})
}

func TestCostOf(t *testing.T) {
	lookup := func(provider, model string) (Price, bool) {
		if provider == "p" && model == "m" {
			return Price{Input: 1, Output: 2}, true
		}
		return Price{}, false
	}
	u := agentcore.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	t.Run("provider cost wins", func(t *testing.T) {
		u := u
		u.CostUSD = 0.25
		assert.Eq(t, 0.25, costOf(lookup, "p", "m", u))
	})
	t.Run("price used when provider reports none", func(t *testing.T) {
		assert.Eq(t, 3.0, costOf(lookup, "p", "m", u))
	})
	t.Run("no price means zero", func(t *testing.T) {
		assert.Eq(t, 0.0, costOf(lookup, "p", "other", u))
		assert.Eq(t, 0.0, costOf(nil, "p", "m", u))
	})
}

// pricedProvider reports usage without a cost, like most providers do.
type pricedProvider struct{ cost float64 }

func (*pricedProvider) Name() string { return "prov" }
func (p *pricedProvider) Generate(context.Context, *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	return &agentcore.GenerateResult{
		Content: "ok", FinishReason: agentcore.FinishFinal,
		Usage: agentcore.Usage{InputTokens: 1_000_000, OutputTokens: 100_000, CacheReadTokens: 400_000, CacheWriteTokens: 100_000, CostUSD: p.cost},
	}, nil
}

// A configured price must reach the model event, the turn usage and the
// session total (store.SumUsage, behind stats and ACP usage_update).
func TestRunnerPricesUsage(t *testing.T) {
	ctx := context.Background()
	price := func(provider, model string) (Price, bool) {
		if provider == "prov" && model == "mod" {
			return Price{Input: 2, Output: 10, CacheRead: 0.5, CacheWrite: 4}, true
		}
		return Price{}, false
	}
	run := func(t *testing.T, provider *pricedProvider, price func(string, string) (Price, bool)) (RunResult, store.Usage) {
		repo, err := store.Open(newStorePath(t))
		assert.NoErr(t, err)
		t.Cleanup(func() { _ = repo.Close() })
		runner, err := New(Config{Repository: repo, Provider: provider, Model: "mod", ProviderName: "prov", Price: price})
		assert.NoErr(t, err)
		res, err := runner.Run(ctx, RunRequest{Prompt: "hi"})
		assert.NoErr(t, err)
		sum, err := repo.SumUsage(ctx, res.SessionID)
		assert.NoErr(t, err)
		return res, sum
	}
	t.Run("priced from config", func(t *testing.T) {
		// plain input 500k*2 + out 100k*10 + read 400k*0.5 + write 100k*4 = 1+1+0.2+0.4
		res, sum := run(t, &pricedProvider{}, price)
		assert.Eq(t, 2.6, math.Round(sum.CostUSD*1e9)/1e9)
		assert.Eq(t, 2.6, math.Round(res.Usage.CostUSD*1e9)/1e9)
		assert.Eq(t, 2.6, math.Round(res.SessionUsage.CostUSD*1e9)/1e9)
	})
	t.Run("provider cost wins", func(t *testing.T) {
		res, sum := run(t, &pricedProvider{cost: 0.75}, price)
		assert.Eq(t, 0.75, sum.CostUSD)
		assert.Eq(t, 0.75, res.Usage.CostUSD)
	})
	t.Run("no price means zero", func(t *testing.T) {
		res, sum := run(t, &pricedProvider{}, nil)
		assert.Eq(t, 0.0, sum.CostUSD)
		assert.Eq(t, 0.0, res.Usage.CostUSD)
	})
}

// TestChildRunUsage: a sub-agent's model calls count in the run in progress
// of the session that started it, without passing the sub-agent's request
// size off as the parent's context size; with no run in progress they are
// held for the session's next run (moved from its base, which already holds
// them, to its run); a run not started as a sub-agent forwards nothing.
func TestChildRunUsage(t *testing.T) {
	repo := openObserveRepo(t)
	runner, err := New(Config{Repository: repo, Provider: &scriptProvider{}})
	assert.Require(t, assert.NoErr(t, err))
	ctx := context.Background()
	used := func(in, out int) agentcore.Usage { return agentcore.Usage{InputTokens: in, OutputTokens: out} }

	t.Run("into the run in progress", func(t *testing.T) {
		var updates []UsageUpdate
		parent := &runUsageMeter{base: used(1000, 0), onModel: func(_ context.Context, u UsageUpdate) { updates = append(updates, u) }}
		defer activateMeter("p1", parent)()
		plain := runner.newRunUsageMeter(ctx, "s-other", "t", nil)
		assert.Eq(t, "", plain.parentSession)
		child := runner.newRunUsageMeter(withChildOf(ctx, "p1"), "s-child", "t", nil)
		assert.Eq(t, "p1", child.parentSession)

		parent.addModel(ctx, used(300, 30), used(300, 30), false)
		priced := used(5000, 50)
		priced.CostUSD = 0.1
		child.addModel(ctx, used(5000, 50), priced, false)
		child.addTool()
		plain.addModel(ctx, used(9, 9), used(9, 9), false)

		run := parent.snapshot()
		assert.Eq(t, 5300, run.Usage.InputTokens)
		assert.Eq(t, 80, run.Usage.OutputTokens)
		assert.Eq(t, 0, run.ToolCalls) // the sub-agent's tool calls stay its own
		assert.Require(t, assert.Len(t, updates, 2))
		assert.Eq(t, 300, updates[0].LastInputTokens)
		assert.Eq(t, 0, updates[1].LastInputTokens) // not the parent's context size
		assert.Eq(t, 6300, updates[1].Session.InputTokens)
		assert.Eq(t, 0.1, updates[1].Run.Usage.CostUSD)
	})
	t.Run("held for the next run", func(t *testing.T) {
		child := runner.newRunUsageMeter(withChildOf(ctx, "p2"), "s-child2", "t", nil)
		child.addModel(ctx, used(400, 40), used(400, 40), false)
		// The next run's base (the session tree total) already holds the call.
		next := &runUsageMeter{base: used(1400, 40)}
		end := activateMeter("p2", next)
		run := next.snapshot()
		assert.Eq(t, 400, run.Usage.InputTokens)
		assert.Eq(t, 1000, next.base.InputTokens)
		assert.Eq(t, 1400, next.base.Add(run.Usage).InputTokens) // the session total does not move
		end()
		again := &runUsageMeter{}
		defer activateMeter("p2", again)()
		assert.Eq(t, 0, again.snapshot().Usage.InputTokens) // taken once
	})
}
