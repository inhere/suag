package native

import (
	"context"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/pkg/agentcore/api"
)

type fakePlanSink struct{ got []Plan }

func (f *fakePlanSink) RecordPlan(_ context.Context, p Plan) error {
	f.got = append(f.got, p)
	return nil
}

func planArgs(steps ...map[string]any) map[string]any {
	items := make([]any, len(steps))
	for i, s := range steps {
		items[i] = s
	}
	return map[string]any{"explanation": "why", "plan": items}
}

func TestUpdatePlanTool(t *testing.T) {
	step := func(s, st string) map[string]any { return map[string]any{"step": s, "status": st} }
	t.Run("valid plan is recorded", func(t *testing.T) {
		sink := &fakePlanSink{}
		res, err := NewUpdatePlanTool(sink).Execute(context.Background(), api.ToolCall{CallID: "c1", Arguments: planArgs(step("a", "completed"), step("b", "in_progress"), step("c", "pending"))})
		assert.NoErr(t, err)
		assert.Eq(t, "update_plan", res.Name)
		assert.Require(t, assert.Eq(t, 1, len(sink.got)))
		assert.Eq(t, "why", sink.got[0].Explanation)
		assert.Eq(t, 3, len(sink.got[0].Steps))
		assert.Eq(t, PlanInProgress, sink.got[0].Steps[1].Status)
	})
	bad := map[string]map[string]any{
		"unknown status":   planArgs(step("a", "done")),
		"empty step":       planArgs(step("  ", "pending")),
		"two in progress":  planArgs(step("a", "in_progress"), step("b", "in_progress")),
		"plan not array":   {"plan": "x"},
		"item not object":  {"plan": []any{"x"}},
		"missing plan key": {},
	}
	for name, args := range bad {
		t.Run(name, func(t *testing.T) {
			sink := &fakePlanSink{}
			_, err := NewUpdatePlanTool(sink).Execute(context.Background(), api.ToolCall{Arguments: args})
			assert.Eq(t, true, err != nil)
			assert.Eq(t, 0, len(sink.got))
		})
	}
	t.Run("missing sink errors", func(t *testing.T) {
		_, err := NewUpdatePlanTool(nil).Execute(context.Background(), api.ToolCall{Arguments: planArgs(step("a", "pending"))})
		assert.Eq(t, true, err != nil)
	})
}
