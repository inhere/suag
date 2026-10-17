package personal

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore/api"
)

func TestRecordApprovalRulePersist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SUGA_HOME", home)
	path := filepath.Join(home, "db", "sessions.db")
	repo, err := store.Open(path)
	assert.Require(t, assert.NoErr(t, err))
	assert.NoErr(t, repo.Close())

	ctx := withObserveScope(context.Background(), "session-1", "turn-1")
	ctx = api.WithSessionID(ctx, "session-1")
	RecordApprovalRulePersist(ctx, "/rules.yaml", "allow", "ok")

	repo, err = store.Open(path)
	assert.Require(t, assert.NoErr(t, err))
	defer repo.Close()
	events, err := repo.ListEvents(context.Background(), store.EventFilter{SessionID: "session-1", Kinds: []string{approvalRulePersistEvent}})
	assert.Require(t, assert.NoErr(t, err))
	assert.Require(t, assert.Len(t, events, 1))
	assert.Eq(t, "turn-1", events[0].TurnID)
	assert.Eq(t, "ok", events[0].Status)
	assert.Eq(t, map[string]any{"file": "/rules.yaml", "action": "allow", "result": "ok"}, events[0].Attrs)
}
