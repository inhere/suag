package chat

import (
	"bytes"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal"
)

func TestRendererBudgetSnapshotFromUsageEvent(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(rendererOptions{Out: &out, Model: "m", now: func() time.Time { return time.Unix(0, 0) }})
	r.BeginTurn()
	snapshot := &personal.BudgetSnapshot{Scope: "turn", NoticeRatio: 0.8,
		Tokens: personal.BudgetMetric{Limit: 100, Used: 85}}
	r.Handle(personal.Event{Kind: "usage", Usage: &personal.UsageUpdate{Budget: snapshot}})
	r.Handle(personal.Event{Kind: "usage", Usage: &personal.UsageUpdate{Budget: snapshot}})
	r.Status(personal.RunResult{}, time.Second, 0)

	assert.Eq(t, 1, bytes.Count(out.Bytes(), []byte("预算提醒：")))
	assert.Contains(t, out.String(), "budget 85%")
}

func TestRendererBudgetSnapshotFromFinishedEvent(t *testing.T) {
	var out bytes.Buffer
	r := newRenderer(rendererOptions{Out: &out, Model: "m", now: func() time.Time { return time.Unix(0, 0) }})
	r.BeginTurn()
	r.Handle(personal.Event{Kind: "run_finished", Usage: &personal.UsageUpdate{Budget: &personal.BudgetSnapshot{
		Scope: "session", Exceeded: true, Reason: "max_tokens",
		Tokens: personal.BudgetMetric{Limit: 5, Used: 12},
	}}})
	r.Status(personal.RunResult{}, time.Second, 0)

	assert.Contains(t, out.String(), "session budget 100%")
	assert.Contains(t, out.String(), "本轮因 token 预算用尽收尾（5/12）")
}
