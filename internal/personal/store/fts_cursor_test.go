package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
)

func TestSearchCursorCodec(t *testing.T) {
	t.Run("round trips the versioned position", func(t *testing.T) {
		want := searchCursor{SnapshotID: "snapshot_01-ab", Offset: 20}
		raw, err := encodeSearchCursor(want)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "v1.snapshot_01-ab.20", raw)

		got, err := decodeSearchCursor(raw)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, want, got)
		parsed, note := parseSearchCursor(raw)
		assert.Eq(t, want, parsed)
		assert.Empty(t, note)
	})

	t.Run("accepts both snapshot boundaries", func(t *testing.T) {
		for _, offset := range []int{0, maxSearchSnapshotHits} {
			raw, err := encodeSearchCursor(searchCursor{SnapshotID: "s", Offset: offset})
			assert.Require(t, assert.NoErr(t, err))
			got, err := decodeSearchCursor(raw)
			assert.Require(t, assert.NoErr(t, err))
			assert.Eq(t, offset, got.Offset)
		}
	})

	t.Run("rejects malformed and untrusted fields", func(t *testing.T) {
		for _, raw := range []string{
			"v2.snapshot.1",
			"v1..1",
			"v1.snapshot",
			"v1.snapshot.1.extra",
			"v1.snapshot/1.1",
			"v1.snapshot 1.1",
			"v1.snapshot.-1",
			"v1.snapshot.+1",
			"v1.snapshot.01",
			"v1.snapshot.501",
			"v1.snapshot.18446744073709551616",
		} {
			t.Run(raw, func(t *testing.T) {
				_, err := decodeSearchCursor(raw)
				assert.Err(t, err)
				_, note := parseSearchCursor(raw)
				assert.Eq(t, searchCursorRestartNote, note)
			})
		}
	})

	t.Run("rejects invalid values when encoding", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			cursor searchCursor
		}{
			{"empty snapshot", searchCursor{Offset: 1}},
			{"unsafe snapshot", searchCursor{SnapshotID: "snapshot.id", Offset: 1}},
			{"negative offset", searchCursor{SnapshotID: "snapshot", Offset: -1}},
			{"offset after snapshot", searchCursor{SnapshotID: "snapshot", Offset: maxSearchSnapshotHits + 1}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := encodeSearchCursor(tc.cursor)
				assert.Err(t, err)
			})
		}
	})
}

func TestEntrySearchFingerprint(t *testing.T) {
	baseTime := time.Date(2026, time.January, 2, 3, 4, 5, 6, time.FixedZone("test", 8*60*60))
	q := EntrySearch{
		Query:            "  中文\t token  ",
		SessionIDs:       []string{"b", "a", "a"},
		CWDPrefix:        "/repo",
		WorkspaceDir:     filepath.Join(string(filepath.Separator), "repo", "."),
		CurrentSessionID: "current",
		FoldCase:         true,
		IncludeAgents:    true,
		Kinds:            []string{EntryTool, EntryUser},
		Since:            baseTime,
		Until:            baseTime.Add(time.Hour),
	}
	base, err := fingerprintEntrySearch(q)
	assert.Require(t, assert.NoErr(t, err))
	assert.Len(t, string(base), 64)

	t.Run("normalizes semantically equivalent fields", func(t *testing.T) {
		equivalent := q
		equivalent.Query = "中文 token"
		equivalent.SessionIDs = []string{"a", "b"}
		equivalent.Kinds = []string{EntryUser, EntryTool, EntryUser}
		equivalent.Since = q.Since.UTC()
		equivalent.Until = q.Until.UTC()
		got, err := fingerprintEntrySearch(equivalent)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, base, got)
	})

	t.Run("changes whenever a result condition changes", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(*EntrySearch)
		}{
			{"query", func(v *EntrySearch) { v.Query = "other" }},
			{"scope", func(v *EntrySearch) { v.WorkspaceDir = "" }},
			{"current session", func(v *EntrySearch) { v.CurrentSessionID = "other" }},
			{"workspace", func(v *EntrySearch) { v.WorkspaceDir = filepath.Join(string(filepath.Separator), "other") }},
			{"case folding", func(v *EntrySearch) { v.FoldCase = false }},
			{"agents", func(v *EntrySearch) { v.IncludeAgents = false }},
			{"kind", func(v *EntrySearch) { v.Kinds = []string{EntryAssistant} }},
			{"since", func(v *EntrySearch) { v.Since = v.Since.Add(time.Nanosecond) }},
			{"until", func(v *EntrySearch) { v.Until = v.Until.Add(time.Nanosecond) }},
			{"cwd prefix", func(v *EntrySearch) { v.CWDPrefix = "/other" }},
			{"session ids", func(v *EntrySearch) { v.SessionIDs = []string{"c"} }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				changed := q
				tc.mutate(&changed)
				got, err := fingerprintEntrySearch(changed)
				assert.Require(t, assert.NoErr(t, err))
				assert.NotEq(t, base, got)
			})
		}
	})

	t.Run("rejects an unknown kind before hashing", func(t *testing.T) {
		q.Kinds = []string{"unknown"}
		_, err := fingerprintEntrySearch(q)
		assert.Err(t, err)
	})
}
