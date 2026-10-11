package personal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
)

// legacyCompactionRecord is the compaction record as an older binary decodes
// it (only the original fields).
type legacyCompactionRecord struct {
	Summary      string `json:"summary"`
	FirstKeptSeq int64  `json:"first_kept_seq"`
	TokensBefore int    `json:"tokens_before"`
	TokensAfter  int    `json:"tokens_after"`
}

// segmentEntries returns the raw compaction entries of a session.
func segmentEntries(t *testing.T, repo *store.Store, sessionID string) []store.Entry {
	t.Helper()
	es, err := repo.ListEntries(context.Background(), sessionID, store.EntryFilter{Types: []string{store.EntryCompaction}})
	assert.NoErr(t, err)
	return es
}

// previousPackage extracts the previous handoff package from a summary
// request (see summarize).
func previousPackage(req *agentcore.GenerateRequest) string {
	text := req.Messages[0].Content
	const head = "# 已有摘要（需并入新摘要）\n"
	i := strings.Index(text, head)
	if i < 0 {
		return ""
	}
	text = text[i+len(head):]
	if j := strings.Index(text, "\n\n# 当前计划"); j >= 0 {
		text = text[:j]
	}
	return text
}

// echoProv makes summaries keep the previous package verbatim
// (as a faithful summarizer folds it in), so a test can follow content
// across several segment breaks.
type echoProv struct {
	*scriptProv
}

func (p echoProv) Generate(ctx context.Context, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	res, err := p.scriptProv.Generate(ctx, req)
	if err != nil || req.System != compactSystemPrompt {
		return res, err
	}
	if prev := previousPackage(req); prev != "" {
		res.Content += "\n<<" + prev + ">>"
	}
	return res, nil
}

func TestNewSessionWritesStartRecord(t *testing.T) {
	repo := openObserveRepo(t)
	src := &memorySource{}
	src.set(memoryLine("remember-me"), nil)
	runner := newMemoryRunner(t, repo, &scriptProv{name: "main"}, src)
	runner.cfg.AppendSystemPrompt = "APPEND-TEXT"
	ctx := context.Background()
	model, err := runner.NewSession(ctx, "seg-new", "t")
	assert.NoErr(t, err)

	es := segmentEntries(t, repo, model.ID)
	assert.Require(t, assert.Eq(t, 1, len(es)))
	assert.Eq(t, "", es[0].TurnID) // outside any turn: adopted at once
	var rec compactionRecord
	assert.NoErr(t, json.Unmarshal(es[0].JSON, &rec))
	assert.Eq(t, segmentReasonStart, rec.Reason)
	assert.Eq(t, "", rec.Summary)
	assert.Eq(t, int64(0), rec.FirstKeptSeq)
	assert.Require(t, assert.NotNil(t, rec.Context))
	assert.Eq(t, "RULES-TEXT", rec.Context.Rules)
	assert.Eq(t, "rules-hash", rec.Context.RulesHash)
	assert.StrContains(t, rec.Context.MemoryIndex, "remember-me")
	assert.Eq(t, metaString(model.Meta, metaMemoryIndexHash), rec.Context.MemoryHash)
	assert.Eq(t, "APPEND-TEXT", rec.Context.AppendPrompt)
	// The hash covers the content and is reproducible.
	sealed := *rec.Context
	sealed.seal()
	assert.Eq(t, rec.Context.Hash, sealed.Hash)
	assert.Eq(t, 64, len(sealed.Hash))

	view, err := runner.loadHistory(ctx, model.ID)
	assert.NoErr(t, err)
	assert.Eq(t, rec.Context.Hash, view.segment.Hash)
	assert.Eq(t, "", view.summary)
	assert.Eq(t, 0, len(view.items))

	t.Run("an older binary sees an empty summary keeping everything", func(t *testing.T) {
		var old legacyCompactionRecord
		assert.NoErr(t, json.Unmarshal(es[0].JSON, &old))
		assert.Eq(t, "", old.Summary)
		assert.Eq(t, int64(0), old.FirstKeptSeq)
	})
}

// A segment record written mid-turn is adopted only with its turn: a failed
// turn's record changes neither the package nor the segment snapshot.
func TestSegmentRecordAdoptedWithTurn(t *testing.T) {
	repo := openObserveRepo(t)
	mem := &memorySource{}
	mem.set(memoryLine("mem-one"), nil)
	mode := "plain"
	var prov *scriptProv
	prov = &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		switch {
		case mode == "plain":
			return finalText("first answer")
		case len(prov.sumReqs) > 0 && mode == "doomed":
			return nil, errors.New("provider exploded")
		case len(prov.sumReqs) > 1 && mode == "ok":
			return finalText("long job done")
		}
		return callTool(fmt.Sprintf("c%d", n))
	}}
	runner := compactRunner(t, repo, prov, bigToolRegistry(2000, nil), func(c *Config) { c.MemoryIndex = mem.index })
	ctx := context.Background()
	first, err := runner.Run(ctx, RunRequest{Prompt: "first question"})
	assert.NoErr(t, err)
	sid := first.SessionID
	start := segmentRecords(t, repo, sid)[0]
	assert.Eq(t, segmentReasonStart, start.Reason)

	// The doomed turn compacts after the memory index changed, then fails.
	mem.set(memoryLine("mem-two"), nil)
	mode = "doomed"
	bad, err := runner.Run(ctx, RunRequest{SessionID: sid, Prompt: "doomed question"})
	assert.Eq(t, true, err != nil)
	assert.Eq(t, RunFailed, bad.State)
	es, err := repo.ListEntries(ctx, sid, store.EntryFilter{TurnID: bad.TurnID, Types: []string{store.EntryCompaction}})
	assert.NoErr(t, err)
	assert.Require(t, assert.True(t, len(es) >= 1))
	var failed compactionRecord
	assert.NoErr(t, json.Unmarshal(es[0].JSON, &failed))
	assert.Eq(t, segmentReasonThreshold, failed.Reason)
	assert.StrContains(t, failed.Context.MemoryIndex, "mem-two")

	view, err := runner.loadHistory(ctx, sid)
	assert.NoErr(t, err)
	assert.Eq(t, start.Context.Hash, view.segment.Hash)
	assert.Eq(t, "", view.summary)

	// A completed turn's mid-turn record is adopted.
	prov.mu.Lock()
	prov.sumReqs = nil
	prov.mu.Unlock()
	mode = "ok"
	good, err := runner.Run(ctx, RunRequest{SessionID: sid, Prompt: "long job"})
	assert.NoErr(t, err)
	es, err = repo.ListEntries(ctx, sid, store.EntryFilter{TurnID: good.TurnID, Types: []string{store.EntryCompaction}})
	assert.NoErr(t, err)
	assert.Require(t, assert.True(t, len(es) >= 1))
	var adopted compactionRecord
	assert.NoErr(t, json.Unmarshal(es[len(es)-1].JSON, &adopted))
	view, err = runner.loadHistory(ctx, sid)
	assert.NoErr(t, err)
	assert.Eq(t, adopted.Context.Hash, view.segment.Hash)
	assert.Eq(t, adopted.Summary, view.summary)
	assert.Eq(t, segmentReasonThreshold, adopted.Reason)

	t.Run("compaction events carry the reason", func(t *testing.T) {
		evs := eventsOf(t, repo, sid, store.EventCompaction)
		assert.Require(t, assert.True(t, len(evs) >= 1))
		assert.Eq(t, segmentReasonThreshold, evs[len(evs)-1].Attrs["reason"])
	})
}

// Sessions written before segment records (no context anywhere) fall back to
// the meta snapshot; an older binary's record after a start record does too.
func TestSegmentLegacySessions(t *testing.T) {
	repo := openObserveRepo(t)
	prov := &scriptProv{name: "main", normal: func(int, *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		return finalText("ok")
	}}
	runner := compactRunner(t, repo, prov, nil, nil)
	ctx := context.Background()
	meta := map[string]any{"personal_rules_content": "OLD-RULES", "personal_rules_hash": "old-hash",
		metaMemoryIndexContent: "OLD-MEMORY", metaMemoryIndexHash: "old-mem", "personal_append_prompt": "OLD-APPEND"}
	legacy := Session{ID: "legacy-1", Title: "t", Status: store.SessionOpen, Meta: meta}
	assert.NoErr(t, repo.CreateSession(ctx, legacy))

	t.Run("no records at all", func(t *testing.T) {
		view, err := runner.loadHistory(ctx, legacy.ID)
		assert.NoErr(t, err)
		assert.Nil(t, view.segment)
		seg := runner.currentSegment(view, meta)
		assert.Eq(t, "OLD-RULES", seg.Rules)
		assert.Eq(t, "OLD-MEMORY", seg.MemoryIndex)
		assert.Eq(t, "OLD-APPEND", seg.AppendPrompt)
		assert.Eq(t, 64, len(seg.Hash))
	})

	_, err := runner.Run(ctx, RunRequest{SessionID: legacy.ID, Prompt: "old question"})
	assert.NoErr(t, err)
	_, err = repo.AppendEntries(ctx, legacy.ID, store.NewEntry{Type: store.EntryCompaction,
		JSON: []byte(`{"summary":"OLD-SUMMARY","first_kept_seq":99,"tokens_before":10,"tokens_after":2}`)})
	assert.NoErr(t, err)

	t.Run("old compaction record without context", func(t *testing.T) {
		view, err := runner.loadHistory(ctx, legacy.ID)
		assert.NoErr(t, err)
		assert.Nil(t, view.segment)
		assert.Eq(t, "OLD-SUMMARY", view.summary)
		assert.Eq(t, 0, len(view.items))
		_, err = runner.Run(ctx, RunRequest{SessionID: legacy.ID, Prompt: "next"})
		assert.NoErr(t, err)
		msgs := prov.reqs[len(prov.reqs)-1].Messages
		assert.Eq(t, 2, len(msgs))
		assert.Eq(t, true, isSegmentContext(msgs[0]))
		assert.Eq(t, summaryMessage("OLD-SUMMARY").Content, msgs[0].Content)
	})

	t.Run("old binary record after a start record", func(t *testing.T) {
		model, err := runner.NewSession(ctx, "mixed-1", "t")
		assert.NoErr(t, err)
		view, err := runner.loadHistory(ctx, model.ID)
		assert.NoErr(t, err)
		assert.NotNil(t, view.segment)
		_, err = repo.AppendEntries(ctx, model.ID, store.NewEntry{Type: store.EntryCompaction,
			JSON: []byte(`{"summary":"S","first_kept_seq":5}`)})
		assert.NoErr(t, err)
		view, err = runner.loadHistory(ctx, model.ID)
		assert.NoErr(t, err)
		assert.Nil(t, view.segment) // the latest record decides; meta is the fallback
		assert.Eq(t, "S", view.summary)
	})
}

// The --from handoff is segment 0's package: every later break gets it as the
// previous package (not as a truncated transcript line), so it survives two
// breaks; an older binary decoding the record still gets the full package.
func TestHandoffSurvivesSegmentBreaks(t *testing.T) {
	repo := openObserveRepo(t)
	sp := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		return finalText(fmt.Sprintf("reply-%d", n))
	}}
	runner := compactRunner(t, repo, echoProv{sp}, nil, func(c *Config) { c.ContextWindow, c.CompactReserveTokens = 100000, 1000 })
	ctx := context.Background()
	src, err := runner.Run(ctx, RunRequest{Prompt: "source work"})
	assert.NoErr(t, err)
	_, err = repo.AppendEntries(ctx, src.SessionID, store.NewEntry{Type: store.EntryPlan, JSON: json.RawMessage(handoffPlan)})
	assert.NoErr(t, err)
	child, err := runner.Handoff(ctx, HandoffRequest{SourceID: src.SessionID, NewID: "child-seg"})
	assert.NoErr(t, err)

	for i := 1; i <= 2; i++ {
		_, err = runner.Run(ctx, RunRequest{SessionID: child.ID, Prompt: fmt.Sprintf("child work %d", i)})
		assert.NoErr(t, err)
		_, err = runner.Compact(ctx, child.ID)
		assert.NoErr(t, err)
	}
	// Both breaks were handed the --from content as the previous package.
	n := len(sp.sumReqs)
	assert.StrContains(t, previousPackage(sp.sumReqs[n-2]), "交接过来")
	assert.StrContains(t, previousPackage(sp.sumReqs[n-1]), "ship T08")

	_, err = runner.Run(ctx, RunRequest{SessionID: child.ID, Prompt: "after two breaks"})
	assert.NoErr(t, err)
	msgs := sp.reqs[len(sp.reqs)-1].Messages
	assert.Eq(t, 2, len(msgs))
	assert.Eq(t, true, isSegmentContext(msgs[0]))
	assert.StrContains(t, msgs[0].Content, "ship T08")
	assert.Eq(t, "after two breaks", msgs[1].Content)

	t.Run("old record structure decodes the full package", func(t *testing.T) {
		es := segmentEntries(t, repo, child.ID)
		var last compactionRecord
		var old legacyCompactionRecord
		assert.NoErr(t, json.Unmarshal(es[len(es)-1].JSON, &last))
		assert.NoErr(t, json.Unmarshal(es[len(es)-1].JSON, &old))
		assert.Eq(t, segmentReasonManual, last.Reason)
		assert.Eq(t, last.Summary, old.Summary)
		assert.Eq(t, last.FirstKeptSeq, old.FirstKeptSeq)
		assert.StrContains(t, old.Summary, "ship T08")
		// What an older binary shows is exactly what this one shows.
		assert.Eq(t, summaryMessage(old.Summary).Content, msgs[0].Content)
	})
}

// The segment context message is rendered by compose only: items() (the
// cut candidates, aligned 1:1 with entries) never contains it.
func TestSegmentContextMessageIsNotAnItem(t *testing.T) {
	user := func(s string, seq int64) ctxItem {
		return ctxItem{msg: agentcore.Message{Role: "user", Content: s}, seq: seq}
	}
	asst := func(s string, seq int64) ctxItem {
		return ctxItem{msg: agentcore.Message{Role: "assistant", Content: s}, seq: seq}
	}
	runner := &Runner{}
	hist := historyView{summary: "PKG", items: []ctxItem{user("u1", 3), asst("a1", 4), user("u2", 5), asst("a2", 6)}}
	b := runner.newContextBuilder("s", "", hist, nil, nil, "")

	view := b.compose(nil)
	assert.Eq(t, len(hist.items)+1, len(view))
	assert.Eq(t, true, isSegmentContext(view[0]))
	assert.Eq(t, "user", view[0].Role)
	assert.StrContains(t, view[0].Content, "PKG")

	items, err := b.items(context.Background(), nil)
	assert.NoErr(t, err)
	assert.Eq(t, len(hist.items), len(items))
	for _, it := range items {
		assert.Eq(t, false, isSegmentContext(it.msg))
	}
	cut := pickCut(items, 6, false) // ~2 short messages kept
	assert.Eq(t, true, cut > 0 && validCut(items, cut))
	assert.Eq(t, false, isSegmentContext(items[cut].msg))

	t.Run("no package, no message", func(t *testing.T) {
		b := runner.newContextBuilder("s", "", historyView{items: hist.items}, nil, nil, "")
		view := b.compose(nil)
		assert.Eq(t, len(hist.items), len(view))
		assert.Eq(t, false, isSegmentContext(view[0]))
	})
	t.Run("a handoff package keeps its own wording", func(t *testing.T) {
		msg, ok := segmentContextMessage("以下是从会话 x 交接过来的上下文", true)
		assert.Eq(t, true, ok)
		assert.Eq(t, "以下是从会话 x 交接过来的上下文", msg.Content)
		assert.Eq(t, true, isSegmentContext(msg))
	})
}

// trace renders sessions with segment records and the new event attributes.
func TestTraceWithSegmentRecords(t *testing.T) {
	repo := openObserveRepo(t)
	prov := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		return finalText(fmt.Sprintf("reply-%d", n))
	}}
	runner := compactRunner(t, repo, prov, nil, func(c *Config) { c.ContextWindow, c.CompactReserveTokens = 100000, 1000 })
	ctx := context.Background()
	res, err := runner.Run(ctx, RunRequest{Prompt: "hello " + strings.Repeat("h", 400)})
	assert.NoErr(t, err)
	_, err = runner.Compact(ctx, res.SessionID)
	assert.NoErr(t, err)
	events, err := repo.ListEvents(ctx, store.EventFilter{SessionID: res.SessionID})
	assert.NoErr(t, err)
	plans, err := repo.ListEntries(ctx, res.SessionID, store.EntryFilter{Types: []string{store.EntryPlan}})
	assert.NoErr(t, err)

	for _, asJSON := range []bool{true, false} {
		t.Run(fmt.Sprintf("json=%v", asJSON), func(t *testing.T) {
			var buf bytes.Buffer
			assert.NoErr(t, WriteTrace(&buf, events, plans, asJSON))
			assert.StrContains(t, buf.String(), "compaction")
			if asJSON {
				assert.StrContains(t, buf.String(), `"reason":"manual"`)
				assert.StrContains(t, buf.String(), `"segment_hash":`)
			}
		})
	}
}
