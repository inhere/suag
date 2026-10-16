package store

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
)

func TestSearchEntriesPageKeepsStableSnapshots(t *testing.T) {
	s := openTemp(t)
	createSession(t, s, "page", "/w")
	entries := make([]NewEntry, 45)
	for i := range entries {
		entries[i] = user(t, fmt.Sprintf("page needle %02d", i))
	}
	appendAll(t, s, "page", entries...)

	query := EntrySearch{Query: "page needle", Limit: 20}
	first, err := s.SearchEntriesPage(context.Background(), query)
	assert.Require(t, assert.NoError(t, err))
	assert.Len(t, first.Hits, 20)
	assert.NotEmpty(t, first.NextCursor)

	seen := make(map[string]bool)
	all := append([]EntryHit(nil), first.Hits...)
	for _, hit := range first.Hits {
		key := fmt.Sprintf("%s/%d", hit.SessionID, hit.Seq)
		assert.False(t, seen[key])
		seen[key] = true
	}
	cursor := first.NextCursor
	for cursor != "" {
		query.Cursor = cursor
		page, err := s.SearchEntriesPage(context.Background(), query)
		assert.Require(t, assert.NoError(t, err))
		all = append(all, page.Hits...)
		for _, hit := range page.Hits {
			key := fmt.Sprintf("%s/%d", hit.SessionID, hit.Seq)
			assert.False(t, seen[key])
			seen[key] = true
		}
		cursor = page.NextCursor
	}
	assert.Len(t, all, 45)
	assert.Len(t, seen, 45)

	// A second query with a larger page still sees the same complete ordered
	// result set, while each cursor page above came only from its snapshot.
	whole, err := s.SearchEntriesPage(context.Background(), EntrySearch{Query: "page needle", Limit: 100})
	assert.Require(t, assert.NoError(t, err))
	assert.Eq(t, all, whole.Hits)
}

func TestSearchEntriesPageSnapshotSurvivesChanges(t *testing.T) {
	s := openTemp(t)
	createSession(t, s, "stable", "/w")
	appendAll(t, s, "stable", user(t, "stable needle first"), user(t, "stable needle second"))

	query := EntrySearch{Query: "stable needle", Limit: 1}
	first, err := s.SearchEntriesPage(context.Background(), query)
	assert.Require(t, assert.NoError(t, err))
	assert.NotEmpty(t, first.NextCursor)
	cursor, err := decodeSearchCursor(first.NextCursor)
	assert.Require(t, assert.NoError(t, err))
	s.searchMu.Lock()
	snapshot := s.searchSnapshots[cursor.SnapshotID]
	want := snapshot.hits[1]
	s.searchMu.Unlock()

	appendAll(t, s, "stable", user(t, "stable needle inserted with a much better repeated stable needle rank"))
	page, err := s.SearchEntriesPage(context.Background(), EntrySearch{Query: query.Query, Limit: 1, Cursor: first.NextCursor})
	assert.Require(t, assert.NoError(t, err))
	assert.Require(t, assert.Len(t, page.Hits, 1))
	assert.Eq(t, want, page.Hits[0])

	// Removing the original row after the first query does not mutate the
	// immutable snapshot; a later recall can decide that the ref is missing.
	_, err = s.DB().ExecContext(context.Background(), `DELETE FROM entries WHERE session_id = ? AND seq = ?`, want.SessionID, want.Seq)
	assert.Require(t, assert.NoError(t, err))
	page, err = s.SearchEntriesPage(context.Background(), EntrySearch{Query: query.Query, Limit: 1, Cursor: first.NextCursor})
	assert.Require(t, assert.NoError(t, err))
	assert.Require(t, assert.Len(t, page.Hits, 1))
	assert.Eq(t, want.Seq, page.Hits[0].Seq)
}

func TestSearchEntriesPageTruncatesSnapshotsExplicitly(t *testing.T) {
	s := openTemp(t)
	createSession(t, s, "large", "/w")
	entries := make([]NewEntry, maxSearchSnapshotHits+1)
	for i := range entries {
		entries[i] = user(t, "large snapshot needle")
	}
	appendAll(t, s, "large", entries...)

	query := EntrySearch{Query: "large snapshot needle", Limit: 100}
	result, err := s.SearchEntriesPage(context.Background(), query)
	assert.Require(t, assert.NoError(t, err))
	assert.Len(t, result.Hits, 100)
	assert.StrContains(t, result.Notice, "500")

	count := len(result.Hits)
	for result.NextCursor != "" {
		query.Cursor = result.NextCursor
		result, err = s.SearchEntriesPage(context.Background(), query)
		assert.Require(t, assert.NoError(t, err))
		count += len(result.Hits)
	}
	assert.Eq(t, maxSearchSnapshotHits, count)
	assert.Empty(t, result.NextCursor)
}

func TestSearchEntriesPageRestartsSafely(t *testing.T) {
	s := openTemp(t)
	createSession(t, s, "restart", "/w")
	appendAll(t, s, "restart", user(t, "restart needle one"), user(t, "different needle"), user(t, "restart needle two"))

	first, err := s.SearchEntriesPage(context.Background(), EntrySearch{Query: "restart needle", Limit: 1})
	assert.Require(t, assert.NoError(t, err))

	t.Run("fingerprint mismatch", func(t *testing.T) {
		result, err := s.SearchEntriesPage(context.Background(), EntrySearch{
			Query: "different needle", Limit: 1, Cursor: first.NextCursor,
		})
		assert.Require(t, assert.NoError(t, err))
		assert.Eq(t, searchCursorRestartNote, result.Notice)
		assert.Require(t, assert.Len(t, result.Hits, 1))
		assert.Eq(t, int64(2), result.Hits[0].Seq)
	})

	t.Run("malformed cursor", func(t *testing.T) {
		result, err := s.SearchEntriesPage(context.Background(), EntrySearch{Query: "restart needle", Limit: 1, Cursor: "v1.bad"})
		assert.Require(t, assert.NoError(t, err))
		assert.Eq(t, searchCursorRestartNote, result.Notice)
		assert.Require(t, assert.Len(t, result.Hits, 1))
	})

	t.Run("expired cursor", func(t *testing.T) {
		cursor, err := decodeSearchCursor(first.NextCursor)
		assert.Require(t, assert.NoError(t, err))
		s.searchMu.Lock()
		s.searchSnapshots[cursor.SnapshotID].createdAt = time.Now().Add(-searchSnapshotTTL - time.Second)
		s.searchMu.Unlock()
		result, err := s.SearchEntriesPage(context.Background(), EntrySearch{Query: "restart needle", Limit: 1, Cursor: first.NextCursor})
		assert.Require(t, assert.NoError(t, err))
		assert.Eq(t, searchCursorRestartNote, result.Notice)
		assert.Require(t, assert.Len(t, result.Hits, 1))
	})
}

func TestSearchEntriesPageLRUAndConcurrency(t *testing.T) {
	s := openTemp(t)
	createSession(t, s, "concurrent", "/w")
	appendAll(t, s, "concurrent", user(t, "concurrent needle"), user(t, "concurrent needle second"))
	first, err := s.SearchEntriesPage(context.Background(), EntrySearch{Query: "concurrent needle", Limit: 1})
	assert.Require(t, assert.NoError(t, err))
	firstCursor, err := decodeSearchCursor(first.NextCursor)
	assert.Require(t, assert.NoError(t, err))

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.SearchEntriesPage(context.Background(), EntrySearch{Query: fmt.Sprintf("concurrent needle %d", i)})
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		assert.NoErr(t, err)
	}

	for i := 0; i < maxSearchSnapshots; i++ {
		_, err := s.SearchEntriesPage(context.Background(), EntrySearch{Query: fmt.Sprintf("lru query %03d", i)})
		assert.Require(t, assert.NoError(t, err))
	}
	s.searchMu.Lock()
	_, retained := s.searchSnapshots[firstCursor.SnapshotID]
	cacheSize := len(s.searchSnapshots)
	s.searchMu.Unlock()
	assert.False(t, retained)
	assert.Eq(t, maxSearchSnapshots, cacheSize)
}
