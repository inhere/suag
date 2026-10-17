package personal

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/internal/personal/store"
)

// tailEvents is what a page that has events up to since does not show yet; the
// server orders events by (started_at, id), so the tail keeps that order.
func tailEvents(events []webEvent, since int64) []webEvent {
	var out []webEvent
	for _, e := range events {
		if e.ID > since {
			out = append(out, e)
		}
	}
	return out
}

// tailEntries is the same for the entry log (ordered by seq).
func tailEntries(entries []traceEntry, since int64) []traceEntry {
	var out []traceEntry
	for _, en := range entries {
		if en.Seq > since {
			out = append(out, en)
		}
	}
	return out
}

func maxEventID(view *TraceView) int64 {
	var max int64
	for _, e := range view.Events {
		if e.ID > max {
			max = e.ID
		}
	}
	return max
}

func maxEntrySeq(view *TraceView) int64 {
	var max int64
	for _, en := range view.Entries {
		if en.Seq > max {
			max = en.Seq
		}
	}
	return max
}

// TestBuildTraceViewSince: an incremental load (since_seq / since_event) sends
// only what the page does not have, with the very fields of the full view: the
// page merges it into what it loaded first, as the auto refresh of the page
// does between two polls.
func TestBuildTraceViewSince(t *testing.T) {
	ctx := context.Background()
	repo, sid := traceRun(t)
	full, err := BuildTraceView(ctx, repo, sid)
	assert.Require(t, assert.NoErr(t, err))
	assert.False(t, full.Delta)
	assert.True(t, len(full.Events) > 2 && len(full.Entries) > 2)

	t.Run("a cut inside the turn links the entries the page already has", func(t *testing.T) {
		// The page holds everything up to the first model event and the first
		// tool call entry; the tool event that follows links the call entry
		// written before the cut (and the model event links the message the
		// call produced), so an incremental load must join over the whole
		// session, not only over its own part.
		var midEvent, midSeq, callSeq int64
		for _, e := range full.Events {
			if e.Kind == store.EventModel && midEvent == 0 {
				midEvent = e.ID
			}
		}
		for _, en := range full.Entries {
			if en.CallID != "" && midSeq == 0 {
				midSeq, callSeq = en.Seq, en.Seq
			}
		}
		assert.True(t, midEvent > 0 && midSeq > 0)
		delta, err := BuildTraceViewSince(ctx, repo, sid, TraceQuery{SinceSeq: midSeq, SinceEvent: midEvent})
		assert.Require(t, assert.NoErr(t, err))
		assert.True(t, delta.Delta)
		assert.Eq(t, tailEvents(full.Events, midEvent), delta.Events)
		assert.Eq(t, tailEntries(full.Entries, midSeq), delta.Entries)
		assert.NotEmpty(t, delta.Events)
		// The tool event of the delta links its call and its result, the call
		// entry being one the page already showed.
		var tool *webEvent
		for i := range delta.Events {
			if delta.Events[i].Kind == store.EventTool {
				tool = &delta.Events[i]
			}
		}
		assert.Require(t, assert.NotNil(t, tool))
		linked := false
		for _, seq := range tool.Entries {
			linked = linked || seq == callSeq
		}
		assert.True(t, linked)
		// The header numbers stay the whole session's.
		assert.Eq(t, full.Analysis, delta.Analysis)
		assert.Eq(t, full.Session, delta.Session)
	})

	t.Run("merging it into the first load yields the full view", func(t *testing.T) {
		// The session moves on while the page is open: the next turn writes an
		// assistant call, its result and the tool event.
		added, err := repo.AppendEntries(ctx, sid,
			store.NewEntry{TurnID: "t2", Type: store.EntryAssistant, JSON: []byte(`{"Role":"assistant","ToolCall":{"CallID":"c9","Name":"shell_exec","Arguments":{"command":"go vet ./..."}}}`)},
			store.NewEntry{TurnID: "t2", Type: store.EntryTool, JSON: []byte(`{"Role":"tool","ToolResult":{"CallID":"c9","Name":"shell_exec","Content":"ok"}}`)},
		)
		assert.Require(t, assert.NoErr(t, err))
		assert.Len(t, added, 2)
		_, err = repo.AppendEvent(ctx, store.Event{SessionID: sid, TurnID: "t2", Kind: store.EventTool, Name: "shell_exec",
			Status: eventOK, StartedAt: time.Now().UTC(), DurationMs: 12, Attrs: map[string]any{"call_id": "c9", "arg": "command=go vet ./..."}})
		assert.Require(t, assert.NoErr(t, err))

		delta, err := BuildTraceViewSince(ctx, repo, sid, TraceQuery{SinceSeq: maxEntrySeq(full), SinceEvent: maxEventID(full)})
		assert.Require(t, assert.NoErr(t, err))
		// Nothing of the first load travels again: the delta holds the new event
		// and the two new entries only.
		assert.Len(t, delta.Events, 1)
		assert.Eq(t, "shell_exec", delta.Events[0].Name)
		assert.Len(t, delta.Entries, 2)
		// The new tool event links its call entry (which the page already has)
		// and its result, exactly as the full view does.
		assert.Eq(t, []int64{added[0].Seq, added[1].Seq}, delta.Events[0].Entries)

		// The merge (what the page does) equals the full view; full2 is read
		// after the delta, so both see the same session state.
		full2, err := BuildTraceView(ctx, repo, sid)
		assert.Require(t, assert.NoErr(t, err))
		merged := &TraceView{Session: full.Session, Analysis: delta.Analysis,
			Events:  append(append([]webEvent{}, full.Events...), delta.Events...),
			Entries: append(append([]traceEntry{}, full.Entries...), delta.Entries...)}
		sort.SliceStable(merged.Events, func(a, b int) bool {
			if merged.Events[a].Start.Equal(merged.Events[b].Start) {
				return merged.Events[a].ID < merged.Events[b].ID
			}
			return merged.Events[a].Start.Before(merged.Events[b].Start)
		})
		assert.Eq(t, full2.Events, merged.Events)
		assert.Eq(t, full2.Entries, merged.Entries)
		assert.Eq(t, full2.Analysis, merged.Analysis)
		assert.Eq(t, full2.Session, delta.Session)
		assert.True(t, len(delta.Entries) < len(full2.Entries))
	})
}
