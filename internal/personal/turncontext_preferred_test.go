package personal

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore/mock"
)

// TestTurnContextPreferredTools: the turn_context event records the preferred
// tools the environment section listed (names only), and omits the attribute
// when the session lists none.
func TestTurnContextPreferredTools(t *testing.T) {
	ctx := context.Background()
	repo, err := store.Open(newStorePath(t))
	assert.Require(t, assert.NoErr(t, err))
	t.Cleanup(func() { _ = repo.Close() })

	names := []string{"rg", "graph"}
	runner, err := New(Config{Repository: repo, Provider: mock.NewProvider(mock.Final("done")), Model: "m", ProviderName: "p",
		EnvironmentSource:    func() string { return "# Environment\n- Preferred tool: rg (command) — search text\n" },
		PreferredToolsSource: func() []string { return names }})
	assert.Require(t, assert.NoErr(t, err))

	res, err := runner.Run(ctx, RunRequest{Prompt: "hi"})
	assert.Require(t, assert.NoErr(t, err))
	events := eventsOf(t, repo, res.SessionID, store.EventTurnContext)
	assert.Require(t, assert.Eq(t, 1, len(events)))
	// Events come back from the store, so the names are a decoded list.
	assert.Eq(t, []any{"rg", "graph"}, events[0].Attrs["preferred_tools"])

	t.Run("session without preferred tools has no attribute", func(t *testing.T) {
		plain, err := New(Config{Repository: repo, Provider: mock.NewProvider(mock.Final("done")), Model: "m", ProviderName: "p",
			PreferredToolsSource: func() []string { return nil }})
		assert.Require(t, assert.NoErr(t, err))
		next, err := plain.Run(ctx, RunRequest{Prompt: "hi"})
		assert.Require(t, assert.NoErr(t, err))
		events := eventsOf(t, repo, next.SessionID, store.EventTurnContext)
		assert.Require(t, assert.Eq(t, 1, len(events)))
		_, found := events[0].Attrs["preferred_tools"]
		assert.False(t, found)
	})
}

// TestTraceShowsPreferredTools: the trace text line and the JSON attrs show the
// names the turn listed, and nothing beyond them.
func TestTraceShowsPreferredTools(t *testing.T) {
	at := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	e := store.Event{Kind: store.EventTurnContext, Name: store.EventTurnContext, Status: eventOK, StartedAt: at,
		Attrs: map[string]any{"model": "m-1", "provider": "p-1", "mode": ModeReadOnly, "tools": "abcdef123456",
			"tools_count": float64(1), "rules_hash": "", "memory_hash": "", "tainted": false,
			"preferred_tools": []any{"rg", "graph"}}}
	var text, js strings.Builder
	assert.NoErr(t, WriteTrace(&text, []store.Event{e}, nil, false))
	assert.StrContains(t, text.String(), "preferred=rg,graph")
	assert.NoErr(t, WriteTrace(&js, []store.Event{e}, nil, true))
	assert.StrContains(t, js.String(), `"preferred_tools":["rg","graph"]`)
}
