package personal

import (
	"context"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore"
)

// TestSessionShortNamePersistsOnce: the name is written at the first chat
// registration and reused by every later resume; a second writer never replaces
// it.
func TestSessionShortNamePersistsOnce(t *testing.T) {
	repo := openObserveRepo(t)
	prov := &scriptProv{name: "main", normal: func(int, *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		return finalText("ok")
	}}
	runner := compactRunner(t, repo, prov, nil, nil)
	ctx := context.Background()
	model, err := runner.NewSession(ctx, "", "New chat")
	assert.Require(t, assert.NoErr(t, err))

	t.Run("a fresh session has no name", func(t *testing.T) {
		name, err := runner.SessionShortName(ctx, model.ID)
		assert.NoErr(t, err)
		assert.Eq(t, "", name)
	})
	t.Run("the name is stored and read back", func(t *testing.T) {
		assert.NoErr(t, runner.SaveSessionShortName(ctx, model.ID, "suga-k7"))
		name, err := runner.SessionShortName(ctx, model.ID)
		assert.NoErr(t, err)
		assert.Eq(t, "suga-k7", name)
	})
	t.Run("a later writer does not replace it", func(t *testing.T) {
		assert.NoErr(t, runner.SaveSessionShortName(ctx, model.ID, "other-aa"))
		name, err := runner.SessionShortName(ctx, model.ID)
		assert.NoErr(t, err)
		assert.Eq(t, "suga-k7", name)
	})
	t.Run("an empty name is ignored", func(t *testing.T) {
		fresh, err := runner.NewSession(ctx, "", "New chat")
		assert.Require(t, assert.NoErr(t, err))
		assert.NoErr(t, runner.SaveSessionShortName(ctx, fresh.ID, "  "))
		name, err := runner.SessionShortName(ctx, fresh.ID)
		assert.NoErr(t, err)
		assert.Eq(t, "", name)
	})
	t.Run("an unknown session reports not found", func(t *testing.T) {
		_, err := runner.SessionShortName(ctx, "nope")
		assert.Err(t, err)
	})
}
