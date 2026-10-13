package personal

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore"
)

// usageSteps answers each call with the next scripted (result, error) pair;
// every result reports usage unless the script leaves it zero.
type usageSteps struct {
	mu    sync.Mutex
	steps []func() (*agentcore.GenerateResult, error)
	n     int
}

func (*usageSteps) Name() string { return "usage-steps" }

func (p *usageSteps) Generate(context.Context, *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	step := p.steps[min(p.n, len(p.steps)-1)]
	p.n++
	return step()
}

func withUsage(res *agentcore.GenerateResult, in, out int) func() (*agentcore.GenerateResult, error) {
	return func() (*agentcore.GenerateResult, error) {
		cp := *res
		cp.Usage = agentcore.Usage{InputTokens: in, OutputTokens: out, TotalTokens: in + out}
		return &cp, nil
	}
}

// usageEvents collects the "usage" and "run_finished" updates of a run.
type usageEvents struct {
	mu      sync.Mutex
	updates []UsageUpdate
	final   *UsageUpdate
}

func (c *usageEvents) emit(_ context.Context, e Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch e.Kind {
	case "usage":
		c.updates = append(c.updates, *e.Usage)
	case "run_finished":
		c.final = e.Usage
	}
	return nil
}

func TestRunUsageCountsAtObserveLayer(t *testing.T) {
	ctx := context.Background()

	t.Run("tool calls include refused ones; session = earlier runs + this run", func(t *testing.T) {
		repo := openObserveRepo(t)
		prov := &usageSteps{steps: []func() (*agentcore.GenerateResult, error){
			withUsage(callStep("c1", "mcp_write", nil), 10, 1), // refused by denyMCP
			withUsage(callStep("c2", "act", nil), 20, 1),
			withUsage(&agentcore.GenerateResult{Content: "done", FinishReason: agentcore.FinishFinal}, 30, 1),
		}}
		ran := 0
		runner := approvalRunner(t, repo, prov, denyMCP, &ran)
		first, err := runner.Run(ctx, RunRequest{Prompt: "go"})
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, RunUsage{Usage: agentcore.Usage{InputTokens: 60, OutputTokens: 3, TotalTokens: 63}, ToolCalls: 2}, first.RunUsage)
		assert.Eq(t, 1, ran)

		// Second turn: the run restarts from zero, the session keeps growing.
		var got usageEvents
		second, err := runner.Run(ctx, RunRequest{SessionID: first.SessionID, Prompt: "again", Emit: got.emit})
		assert.Require(t, assert.NoErr(t, err))
		assert.Require(t, assert.Eq(t, 1, len(got.updates)))
		u := got.updates[0]
		assert.Eq(t, 30, u.Run.Usage.InputTokens) // the provider's last step repeats
		assert.Eq(t, 30, u.LastInputTokens)
		assert.Eq(t, 90, u.Session.InputTokens)
		assert.Eq(t, 4, u.Session.OutputTokens)
		assert.Require(t, assert.NotNil(t, got.final))
		assert.Eq(t, second.RunUsage, got.final.Run)
		assert.Eq(t, u.Run, got.final.Run)
		assert.Eq(t, second.SessionUsage.InputTokens, got.final.Session.InputTokens)
	})

	t.Run("a failed attempt with usage counts, one without does not", func(t *testing.T) {
		repo := openObserveRepo(t)
		prov := &usageSteps{steps: []func() (*agentcore.GenerateResult, error){
			func() (*agentcore.GenerateResult, error) {
				return &agentcore.GenerateResult{Usage: agentcore.Usage{InputTokens: 7, OutputTokens: 2}}, errors.New("boom")
			},
		}}
		var got usageEvents
		res, err := compactRunner(t, repo, prov, nil, nil).Run(ctx, RunRequest{Prompt: "x", Emit: got.emit})
		assert.Err(t, err)
		assert.Eq(t, 7, res.RunUsage.Usage.InputTokens)
		assert.Eq(t, 1, len(got.updates))

		repo = openObserveRepo(t)
		prov = &usageSteps{steps: []func() (*agentcore.GenerateResult, error){
			func() (*agentcore.GenerateResult, error) { return nil, context.Canceled },
		}}
		got = usageEvents{}
		res, err = compactRunner(t, repo, prov, nil, nil).Run(ctx, RunRequest{Prompt: "x", Emit: got.emit})
		assert.Err(t, err)
		assert.Eq(t, RunUsage{}, res.RunUsage)
		assert.Eq(t, 0, len(got.updates))
		assert.Require(t, assert.NotNil(t, got.final))
		assert.Eq(t, RunUsage{}, got.final.Run)
	})

	t.Run("reported shape always carries every field", func(t *testing.T) {
		rep := ReportRunUsage(RunUsage{Usage: agentcore.Usage{InputTokens: 14, OutputTokens: 6, CacheReadTokens: 4}, ToolCalls: 1})
		assert.Eq(t, ReportedRunUsage{InputTokens: 10, OutputTokens: 6, CacheReadTokens: 4, TotalTokens: 20, ToolCalls: 1}, rep)
	})
}
