package personal

import (
	"context"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore/mock"
)

// TestTurnContextAddDirs: the turn_context event records the session's
// additional directories (--add-dir) as the effective "path:ro"/"path:rw"
// entries, and omits the attribute when there is none.
func TestTurnContextAddDirs(t *testing.T) {
	ctx := context.Background()
	repo, err := store.Open(newStorePath(t))
	assert.Require(t, assert.NoErr(t, err))
	t.Cleanup(func() { _ = repo.Close() })

	dirs := []string{"/work/docs:ro", "/work/shared:rw"}
	runner, err := New(Config{Repository: repo, Provider: mock.NewProvider(mock.Final("done")), Model: "m", ProviderName: "p",
		AddDirs: func() []string { return dirs }})
	assert.Require(t, assert.NoErr(t, err))

	res, err := runner.Run(ctx, RunRequest{Prompt: "hi"})
	assert.Require(t, assert.NoErr(t, err))
	events := eventsOf(t, repo, res.SessionID, store.EventTurnContext)
	assert.Require(t, assert.Eq(t, 1, len(events)))
	assert.Eq(t, []any{"/work/docs:ro", "/work/shared:rw"}, events[0].Attrs["add_dirs"])

	t.Run("no added directories, no attribute", func(t *testing.T) {
		plain, err := New(Config{Repository: repo, Provider: mock.NewProvider(mock.Final("done")), Model: "m", ProviderName: "p"})
		assert.Require(t, assert.NoErr(t, err))
		next, err := plain.Run(ctx, RunRequest{Prompt: "hi"})
		assert.Require(t, assert.NoErr(t, err))
		events := eventsOf(t, repo, next.SessionID, store.EventTurnContext)
		assert.Require(t, assert.Eq(t, 1, len(events)))
		_, found := events[0].Attrs["add_dirs"]
		assert.False(t, found)
	})
}
