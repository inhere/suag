package store

import (
	"context"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
)

func TestMemoryFTSSyncAndSearch(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	updated := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	sources := []MemorySource{
		{ID: "title-hit", Text: "needle\nshort body", Updated: updated},
		{ID: "body-hit", Text: "ordinary title\nneedle plus a much longer body with unrelated words", Updated: updated},
		{ID: "中文", Text: "中文标题\n正文", Updated: updated},
		{ID: "one-rune", Text: "中\nshort", Updated: updated},
	}

	t.Run("sync is incremental and scoped", func(t *testing.T) {
		added, removed, err := s.SyncMemoryIndex(ctx, "project:p", sources)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, 4, added)
		assert.Eq(t, 0, removed)
		added, removed, err = s.SyncMemoryIndex(ctx, "project:p", sources)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, 0, added)
		assert.Eq(t, 0, removed)
		_, _, err = s.SyncMemoryIndex(ctx, "user", []MemorySource{{ID: "other", Text: "needle", Updated: updated}})
		assert.Require(t, assert.NoErr(t, err))
		var n int
		assert.NoErr(t, s.db.QueryRow(`SELECT COUNT(1) FROM memory_fts WHERE scope = ?`, "project:p").Scan(&n))
		assert.Eq(t, 4, n)
	})

	t.Run("bm25 ranks a title hit before a frequent body hit", func(t *testing.T) {
		hits, err := s.SearchMemoryIndex(ctx, "project:p", "needle", 10)
		assert.Require(t, assert.NoErr(t, err))
		assert.Require(t, assert.Len(t, hits, 2))
		assert.Eq(t, "title-hit", hits[0].ID)
		assert.StrContains(t, hits[0].Snippet, "needle")
	})
	t.Run("short Chinese query and one-rune query use LIKE", func(t *testing.T) {
		hits, err := s.SearchMemoryIndex(ctx, "project:p", "中文", 10)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, []string{"中文"}, memoryHitIDs(hits))
		hits, err = s.SearchMemoryIndex(ctx, "project:p", "中", 10)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, []string{"one-rune", "中文"}, memoryHitIDs(hits))
	})
	t.Run("changed and removed rows are reflected", func(t *testing.T) {
		changed := append([]MemorySource(nil), sources[:2]...)
		changed[0].Text = "needle\nchanged content"
		added, removed, err := s.SyncMemoryIndex(ctx, "project:p", changed)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, 1, added)
		assert.Eq(t, 3, removed)
		hits, err := s.SearchMemoryIndex(ctx, "project:p", "changed", 10)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, []string{"title-hit"}, memoryHitIDs(hits))
		hits, err = s.SearchMemoryIndex(ctx, "project:p", "中文", 10)
		assert.Require(t, assert.NoErr(t, err))
		assert.Empty(t, hits)
	})
}

func memoryHitIDs(hits []MemoryHit) []string {
	ids := make([]string, len(hits))
	for i, hit := range hits {
		ids[i] = hit.ID
	}
	return ids
}

func TestSearchMemoryIndexEmptyQuery(t *testing.T) {
	s := openTemp(t)
	_, err := s.SearchMemoryIndex(context.Background(), "user", " ", 10)
	assert.Err(t, err)
}
