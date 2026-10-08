package personal

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/data/testutil"
	"github.com/inhere/suag/internal/host/session"
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

func TestUsageMetadataRoundTripsThroughJSON(t *testing.T) {
	want := agentcore.Usage{InputTokens: 14, OutputTokens: 6, TotalTokens: 20, CacheReadTokens: 4, CacheWriteTokens: 2, CostUSD: 0.5}
	data, err := json.Marshal(map[string]any{usageMetadataKey: usageToMetadata(want)})
	assert.NoErr(t, err)
	var decoded map[string]any
	assert.NoErr(t, json.Unmarshal(data, &decoded))
	assert.Eq(t, want, usageFromMetadata(decoded))
	assert.Eq(t, agentcore.Usage{}, usageFromMetadata(nil))
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
	repo, err := session.NewSQLiteRepository(testutil.NewMigratedSQLitePath(t, "sessions.db"))
	assert.NoErr(t, err)
	t.Cleanup(func() { _ = repo.Close() })
	runRoot := filepath.Join(t.TempDir(), "runs")
	newRunner := func() *Runner {
		runner, err := New(Config{Repository: repo, RunRoot: runRoot, Provider: &usageProvider{}, LockRoot: t.TempDir()})
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

	stored, err := repo.GetSession(ctx, "usage-session")
	assert.NoErr(t, err)
	assert.Eq(t, second.SessionUsage, usageFromMetadata(stored.Metadata))
}

func TestRunnerSessionUsageCountsCancelledTurn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repo, err := session.NewSQLiteRepository(testutil.NewMigratedSQLitePath(t, "sessions.db"))
	assert.NoErr(t, err)
	t.Cleanup(func() { _ = repo.Close() })
	runner, err := New(Config{Repository: repo, RunRoot: filepath.Join(t.TempDir(), "runs"), Provider: &usageProvider{},
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
