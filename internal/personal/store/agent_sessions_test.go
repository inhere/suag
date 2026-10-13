package store

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
)

// agentMeta is the meta of a sub-agent session started by parent.
func agentMeta(parent string) map[string]any {
	return map[string]any{MetaKind: KindAgent, MetaParentSession: parent}
}

func TestAgentSessions(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	assert.Require(t, assert.NoErr(t, s.CreateSession(ctx, Session{ID: "p", Cwd: "/w", Status: SessionOpen})))
	assert.Require(t, assert.NoErr(t, s.CreateSession(ctx, Session{ID: "c", Cwd: "/w", Status: SessionOpen, Meta: agentMeta("p")})))
	assert.Require(t, assert.NoErr(t, s.CreateSession(ctx, Session{ID: "other", Cwd: "/w", Status: SessionOpen})))

	t.Run("IsAgentSession and ParentSession", func(t *testing.T) {
		c, err := s.GetSession(ctx, "c")
		assert.Require(t, assert.NoErr(t, err))
		assert.True(t, IsAgentSession(c))
		assert.Eq(t, "p", ParentSession(c))
		p, _ := s.GetSession(ctx, "p")
		assert.False(t, IsAgentSession(p))
	})
	t.Run("SumUsageTree adds the sub-agent sessions", func(t *testing.T) {
		for _, e := range []Event{
			{SessionID: "p", Kind: EventModel, InputTokens: 100, OutputTokens: 10, Attrs: map[string]any{"cost_usd": 0.5}},
			{SessionID: "c", Kind: EventModel, InputTokens: 40, OutputTokens: 4, Attrs: map[string]any{"cost_usd": 0.25}},
			{SessionID: "c", Kind: EventTool, InputTokens: 999}, // not a model event
			{SessionID: "other", Kind: EventModel, InputTokens: 7},
		} {
			_, err := s.AppendEvent(ctx, e)
			assert.Require(t, assert.NoErr(t, err))
		}
		own, err := s.SumUsage(ctx, "p")
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, int64(100), own.InputTokens)
		tree, err := s.SumUsageTree(ctx, "p")
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, int64(2), tree.Calls)
		assert.Eq(t, int64(140), tree.InputTokens)
		assert.Eq(t, int64(14), tree.OutputTokens)
		assert.Eq(t, 0.75, tree.CostUSD)
	})
	t.Run("SessionCwds leaves them out", func(t *testing.T) {
		cwds, err := s.SessionCwds(ctx)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, map[string]string{"p": "/w", "other": "/w"}, cwds)
	})
	t.Run("SearchEntries leaves them out unless asked", func(t *testing.T) {
		for _, id := range []string{"p", "c"} {
			_, err := s.AppendEntries(ctx, id, user(t, "needle in "+id))
			assert.Require(t, assert.NoErr(t, err))
		}
		hits, err := s.SearchEntries(ctx, EntrySearch{Query: "needle"})
		assert.Require(t, assert.NoErr(t, err))
		assert.Require(t, assert.Len(t, hits, 1))
		assert.Eq(t, "p", hits[0].SessionID)
		hits, err = s.SearchEntries(ctx, EntrySearch{Query: "needle", IncludeAgents: true})
		assert.Require(t, assert.NoErr(t, err))
		assert.Len(t, hits, 2)
	})
}

func TestCollectGarbageAgentSessions(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	old, recent := now.Add(-48*time.Hour), now.Add(-time.Hour)
	cutoff := now.Add(-24 * time.Hour)
	s := openTemp(t)
	for _, sess := range []Session{
		{ID: "p-old", UpdatedAt: old},
		{ID: "c-of-old", UpdatedAt: recent, Meta: agentMeta("p-old")}, // goes with its parent, however recent
		{ID: "p-new", UpdatedAt: recent},
		{ID: "c-of-new", UpdatedAt: old, Meta: agentMeta("p-new")}, // stays with its parent, however old
		{ID: "orphan-old", UpdatedAt: old, Meta: agentMeta("gone")},
		{ID: "orphan-new", UpdatedAt: recent, Meta: agentMeta("gone")},
	} {
		sess.CreatedAt = sess.UpdatedAt
		assert.Require(t, assert.NoErr(t, s.CreateSession(ctx, sess)))
	}
	report, err := s.CollectGarbage(ctx, cutoff, nil, true)
	assert.Require(t, assert.NoErr(t, err))
	var removed []string
	for _, sess := range report.Sessions {
		removed = append(removed, sess.ID)
	}
	slices.Sort(removed)
	assert.Eq(t, []string{"c-of-old", "orphan-old", "p-old"}, removed)
	left, err := s.ListSessions(ctx)
	assert.Require(t, assert.NoErr(t, err))
	var ids []string
	for _, sess := range left {
		ids = append(ids, sess.ID)
	}
	slices.Sort(ids)
	assert.Eq(t, []string{"c-of-new", "orphan-new", "p-new"}, ids)
}
