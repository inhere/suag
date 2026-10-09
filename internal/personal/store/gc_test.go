package store

import (
	"context"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
)

func TestCollectGarbage(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	old, recent := now.Add(-48*time.Hour), now.Add(-time.Hour)
	cutoff := now.Add(-24 * time.Hour)

	setup := func(t *testing.T) *Store {
		s := openTemp(t)
		for id, at := range map[string]time.Time{"stale": old, "busy": old, "fresh": recent} {
			assert.Require(t, assert.NoErr(t, s.CreateSession(ctx, Session{ID: id, CreatedAt: at, UpdatedAt: at})))
			_, err := s.AppendEntries(ctx, id, NewEntry{Type: "message", JSON: []byte(`{"text":"x"}`)})
			assert.Require(t, assert.NoErr(t, err))
			// AppendEntries bumps updated_at to now; age the session afterwards.
			_, err = s.DB().ExecContext(ctx, `UPDATE sessions SET updated_at = ? WHERE id = ?`, formatTime(at), id)
			assert.Require(t, assert.NoErr(t, err))
		}
		for _, e := range []Event{
			{SessionID: "stale", Kind: EventModel, StartedAt: recent}, // removed with its session
			{SessionID: "fresh", Kind: EventModel, StartedAt: old},    // kept: its session stays
			{SessionID: "gone", Kind: EventModel, StartedAt: old},     // orphan and old: removed
			{SessionID: "gone", Kind: EventModel, StartedAt: recent},  // orphan but recent: kept
		} {
			_, err := s.AppendEvent(ctx, e)
			assert.Require(t, assert.NoErr(t, err))
		}
		return s
	}
	keep := map[string]bool{"busy": true}

	t.Run("dry run lists and writes nothing", func(t *testing.T) {
		s := setup(t)
		report, err := s.CollectGarbage(ctx, cutoff, keep, false)
		assert.NoErr(t, err)
		assert.Eq(t, false, report.Applied)
		assert.Eq(t, 1, len(report.Sessions))
		assert.Eq(t, "stale", report.Sessions[0].ID)
		assert.Eq(t, int64(2), report.Events)
		_, err = s.GetSession(ctx, "stale")
		assert.NoErr(t, err)
	})

	t.Run("apply removes stale sessions, their rows and old orphan events", func(t *testing.T) {
		s := setup(t)
		report, err := s.CollectGarbage(ctx, cutoff, keep, true)
		assert.NoErr(t, err)
		assert.Eq(t, true, report.Applied)
		assert.Eq(t, int64(2), report.Events)
		_, err = s.GetSession(ctx, "stale")
		assert.ErrIs(t, err, ErrSessionNotFound)
		entries, err := s.ListEntries(ctx, "stale", EntryFilter{})
		assert.NoErr(t, err)
		assert.Empty(t, entries) // cascade
		for _, id := range []string{"busy", "fresh"} {
			_, err = s.GetSession(ctx, id)
			assert.NoErr(t, err)
		}
		var left int
		assert.NoErr(t, s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&left))
		assert.Eq(t, 2, left) // fresh's event and the recent orphan
	})

	t.Run("nothing to do", func(t *testing.T) {
		s := openTemp(t)
		report, err := s.CollectGarbage(ctx, cutoff, nil, true)
		assert.NoErr(t, err)
		assert.Eq(t, 0, len(report.Sessions))
		assert.Eq(t, int64(0), report.Events)
	})
}
