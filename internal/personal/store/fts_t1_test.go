package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore/api"
)

func t1NamedTool(t *testing.T, name, content string) NewEntry {
	t.Helper()
	call := &api.ToolCall{CallID: "t1-call", Name: name}
	msg := api.Message{
		Role:       "tool",
		Content:    content,
		ToolCall:   call,
		ToolResult: &api.ToolResult{CallID: "t1-call", Name: name, Content: content},
	}
	raw, err := json.Marshal(msg)
	assert.Require(t, assert.NoErr(t, err))
	return NewEntry{Type: EntryTool, JSON: raw}
}

func TestFTSShortAndLongTermsStayAND(t *testing.T) {
	s := openTemp(t)
	createSession(t, s, "s1", "/w")
	appendAll(t, s, "s1",
		user(t, "token only"),
		user(t, "中文 token"),
		user(t, "中"),
		user(t, "数据库索引"),
	)

	t.Run("mixed query keeps the short term filter", func(t *testing.T) {
		hits := search(t, s, EntrySearch{Query: "中文 token"})
		assert.Eq(t, []string{"s1/2"}, hitKeys(hits))
	})
	t.Run("one rune term uses LIKE semantics", func(t *testing.T) {
		hits := search(t, s, EntrySearch{Query: "中"})
		assert.Eq(t, []string{"s1/2", "s1/3"}, hitKeys(hits))
	})
	t.Run("three rune term remains searchable", func(t *testing.T) {
		hits := search(t, s, EntrySearch{Query: "数据库"})
		assert.Eq(t, []string{"s1/4"}, hitKeys(hits))
	})
}

func TestFTSMixedQueryUsesMatchOrdering(t *testing.T) {
	s := openTemp(t)
	createSession(t, s, "s1", "/w")
	appendAll(t, s, "s1", user(t, "中 needle needle needle"), user(t, "中 needle"))

	// Match ranking puts the more selective long-term hit first; all-LIKE
	// fallback would use created_at DESC and return the newer second row first.
	hits := search(t, s, EntrySearch{Query: "中 needle"})
	assert.Require(t, assert.Len(t, hits, 2))
	assert.Eq(t, int64(1), hits[0].Seq)
	assert.Eq(t, int64(2), hits[1].Seq)
}

func TestFTSKindAndTimeFilters(t *testing.T) {
	s := openTemp(t)
	createSession(t, s, "s1", "/w")
	entries := appendAll(t, s, "s1",
		user(t, "kindneedle"),
		assistant(t, "kindneedle"),
		t1NamedTool(t, "shell", "kindneedle"),
		NewEntry{Type: EntryCompaction, JSON: json.RawMessage(`{"summary":"kindneedle"}`)},
	)
	assert.Len(t, entries, 4)

	t.Run("each indexed kind and multiple kinds", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			kinds []string
			want  []string
		}{
			{"user", []string{EntryUser}, []string{"s1/1"}},
			{"assistant", []string{EntryAssistant}, []string{"s1/2"}},
			{"tool", []string{EntryTool}, []string{"s1/3"}},
			{"compaction", []string{EntryCompaction}, []string{"s1/4"}},
			{"multiple", []string{EntryUser, EntryAssistant}, []string{"s1/1", "s1/2"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				assert.Eq(t, tc.want, hitKeys(search(t, s, EntrySearch{Query: "kindneedle", Kinds: tc.kinds})))
			})
		}
	})
	t.Run("invalid kind is rejected", func(t *testing.T) {
		_, err := s.SearchEntries(context.Background(), EntrySearch{Query: "kindneedle", Kinds: []string{"unknown"}})
		assert.Err(t, err)
		_, err = s.SearchEntries(context.Background(), EntrySearch{Query: "kindneedle", Kinds: []string{""}})
		assert.Err(t, err)
	})

	base := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	for i := range entries {
		at := base.AddDate(0, 0, i)
		_, err := s.DB().ExecContext(context.Background(), `UPDATE entries SET created_at = ? WHERE session_id = ? AND seq = ?`, formatTime(at), "s1", entries[i].Seq)
		assert.Require(t, assert.NoErr(t, err))
	}
	t.Run("since is inclusive and until is exclusive", func(t *testing.T) {
		query := EntrySearch{Query: "kindneedle", Since: base.AddDate(0, 0, 1), Until: base.AddDate(0, 0, 3)}
		assert.Eq(t, []string{"s1/2", "s1/3"}, hitKeys(search(t, s, query)))
		assert.Eq(t, []string{"s1/4"}, hitKeys(search(t, s, EntrySearch{Query: "kindneedle", Since: base.AddDate(0, 0, 3)})))
	})
}

func TestFTSSearchExcludesRetrievalToolTranscripts(t *testing.T) {
	s := openTemp(t)
	createSession(t, s, "s1", "/w")
	appendAll(t, s, "s1",
		t1NamedTool(t, "shell", "searchpollution"),
		t1NamedTool(t, "session_search", "searchpollution"),
		t1NamedTool(t, "recall", "searchpollution"),
	)

	hits := search(t, s, EntrySearch{Query: "searchpollution"})
	assert.Eq(t, []string{"s1/1"}, hitKeys(hits))
	assert.Eq(t, EntryTool, hits[0].Role)
}
