package personal

import (
	"context"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/tools"
	"github.com/inhere/suag/pkg/agentcore/tools/native"
)

// TestUpdatePlanPersistsEmitsAndFeedsCompaction: update_plan writes a plan
// entry for the turn, emits a plan event, and the latest plan reaches the
// compaction summary request.
func TestUpdatePlanPersistsEmitsAndFeedsCompaction(t *testing.T) {
	repo := openObserveRepo(t)
	plans := NewPlanRecorder()
	reg := tools.NewRegistry()
	reg.Register(native.NewUpdatePlanTool(plans))
	prov := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		if n == 1 {
			return &agentcore.GenerateResult{
				ToolCalls: []agentcore.ToolCall{{CallID: "p1", Name: "update_plan", Arguments: map[string]any{
					"plan": []any{map[string]any{"step": "ship T07", "status": "in_progress"}},
				}}},
				FinishReason: agentcore.FinishToolCalls,
			}, nil
		}
		return finalText("done " + strings.Repeat("d", 2000))
	}}
	runner := compactRunner(t, repo, prov, reg, func(c *Config) {
		c.Plans, c.ContextWindow, c.CompactReserveTokens = plans, 100000, 1000
	})
	var events []Event
	res, err := runner.Run(context.Background(), RunRequest{Prompt: "go", Emit: func(_ context.Context, e Event) error {
		events = append(events, e)
		return nil
	}})
	assert.NoErr(t, err)

	t.Run("plan event", func(t *testing.T) {
		var got []Event
		for _, e := range events {
			if e.Kind == "plan" {
				got = append(got, e)
			}
		}
		assert.Require(t, assert.Eq(t, 1, len(got)))
		assert.Eq(t, res.RunID, got[0].RunID)
		assert.Eq(t, "ship T07", got[0].Plan.Steps[0].Step)
	})
	t.Run("plan entry belongs to the turn", func(t *testing.T) {
		e, err := repo.LatestEntry(context.Background(), res.SessionID, store.EntryPlan)
		assert.NoErr(t, err)
		assert.Require(t, assert.Eq(t, true, e != nil))
		assert.Eq(t, res.TurnID, e.TurnID)
	})
	t.Run("summary request carries latest plan", func(t *testing.T) {
		_, err := runner.Compact(context.Background(), res.SessionID)
		assert.NoErr(t, err)
		assert.Require(t, assert.Eq(t, 1, len(prov.sumReqs)))
		assert.Eq(t, true, strings.Contains(prov.sumReqs[0].Messages[0].Content, "ship T07"))
	})
	t.Run("unbound recorder fails", func(t *testing.T) {
		assert.Eq(t, true, NewPlanRecorder().RecordPlan(context.Background(), native.Plan{}) != nil)
	})
}
