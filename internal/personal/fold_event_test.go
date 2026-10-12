package personal

import (
	"context"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/mock"
	"github.com/inhere/suag/pkg/agentcore/tools"
)

// TestFoldEventsAcrossTurns: a boundary move is recorded once, also when it
// is first seen by a later turn's builder, and the baseline is kept in the
// session meta.
func TestFoldEventsAcrossTurns(t *testing.T) {
	repo := openObserveRepo(t)
	registry := tools.NewRegistry()
	registry.Register(fnTool{name: "fine", fn: func() (*agentcore.ToolResult, error) { return &agentcore.ToolResult{Content: "ok"}, nil }})
	call := func(id string) mock.Step { return mock.ToolCall(id, "fine", map[string]any{}) }
	runner, err := New(Config{Repository: repo, Tools: registry, FoldEvery: 2, FoldKeep: 1, Provider: mock.NewProvider(
		// turn 1: steps before each request 0,1,2,3 -> boundary 0,0,0,2
		call("c1"), call("c2"), call("c3"), mock.Final("first"),
		// turn 2 starts with 4 steps (boundary 2, already recorded), then 5 -> 4
		call("c4"), mock.Final("second"),
	)})
	assert.NoErr(t, err)
	ctx := context.Background()
	first, err := runner.Run(ctx, RunRequest{Prompt: "go"})
	assert.Require(t, assert.NoErr(t, err))
	_, err = runner.Run(ctx, RunRequest{SessionID: first.SessionID, Prompt: "more"})
	assert.Require(t, assert.NoErr(t, err))

	events := eventsOf(t, repo, first.SessionID, store.EventFold)
	assert.Require(t, assert.Eq(t, 2, len(events)))
	assert.Eq(t, float64(2), events[0].Attrs["boundary"])
	assert.Eq(t, float64(3), events[0].Attrs["steps"])
	assert.Eq(t, float64(4), events[1].Attrs["boundary"])
	before, _ := events[1].Attrs["chars_before"].(float64)
	after, _ := events[1].Attrs["chars_after"].(float64)
	assert.True(t, after <= before, "chars %v -> %v", before, after)
	assert.StrContains(t, traceLine(events[1]), "fold fold ok")
	assert.StrContains(t, traceLine(events[1]), "folded=4/5")

	sess, err := repo.GetSession(ctx, first.SessionID)
	assert.NoErr(t, err)
	assert.Eq(t, 4, metaCount(sess.Meta[metaFoldBoundary]))

	t.Run("a new segment resets the baseline", func(t *testing.T) {
		assert.Eq(t, 0, segmentMeta(&segmentContext{})[metaFoldBoundary])
	})
}
