package personal

import (
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

const handoffPlan = `{"plan":[{"step":"ship T08","status":"in_progress"}]}`

// handoffSource creates a source session with one completed turn, a plan
// entry and inherited-tool meta.
func handoffSource(t *testing.T) (*store.Store, *Runner, *scriptProv, string) {
	t.Helper()
	repo := openObserveRepo(t)
	prov := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		return finalText(fmt.Sprintf("reply-%d", n))
	}}
	runner := compactRunner(t, repo, prov, nil, func(c *Config) { c.ContextWindow, c.CompactReserveTokens = 100000, 1000 })
	res, err := runner.Run(context.Background(), RunRequest{Prompt: "source work"})
	assert.NoErr(t, err)
	_, err = repo.AppendEntries(context.Background(), res.SessionID, store.NewEntry{Type: store.EntryPlan, JSON: json.RawMessage(handoffPlan)})
	assert.NoErr(t, err)
	assert.NoErr(t, repo.PatchSessionMeta(context.Background(), res.SessionID, map[string]any{metaLoadedTools: []string{"mcp_a", "mcp_b"}}))
	return repo, runner, prov, res.SessionID
}

func entryCount(t *testing.T, repo *store.Store, id string) int {
	t.Helper()
	es, err := repo.ListEntries(context.Background(), id, store.EntryFilter{})
	assert.NoErr(t, err)
	return len(es)
}

func TestHandoffGeneratesSummaryWithoutTouchingSource(t *testing.T) {
	repo, runner, prov, src := handoffSource(t)
	ctx := context.Background()
	beforeEntries := entryCount(t, repo, src)
	before, err := repo.GetSession(ctx, src)
	assert.NoErr(t, err)

	model, err := runner.Handoff(ctx, HandoffRequest{SourceID: src, NewID: "child-1"})
	assert.NoErr(t, err)
	assert.Eq(t, "child-1", model.ID)
	assert.Eq(t, 1, len(prov.sumReqs))
	assert.Eq(t, true, strings.Contains(prov.sumReqs[0].Messages[0].Content, "source work"))

	t.Run("source is unchanged", func(t *testing.T) {
		assert.Eq(t, beforeEntries, entryCount(t, repo, src))
		after, err := repo.GetSession(ctx, src)
		assert.NoErr(t, err)
		assert.Eq(t, before.Meta, after.Meta)
		assert.Eq(t, before.Status, after.Status)
		assert.Eq(t, 0, len(compactionEntries(t, repo, src)))
	})
	t.Run("handoff entry and inherited tools", func(t *testing.T) {
		// The new session's segment start record, then the handoff.
		es, err := repo.ListEntries(ctx, "child-1", store.EntryFilter{})
		assert.NoErr(t, err)
		assert.Eq(t, 2, len(es))
		assert.Eq(t, store.EntryCompaction, es[0].Type)
		assert.Eq(t, store.EntryHandoff, es[1].Type)
		var rec handoffRecord
		assert.NoErr(t, json.Unmarshal(es[1].JSON, &rec))
		assert.Eq(t, src, rec.Source)
		assert.Eq(t, "SUMMARY-main-1", rec.Summary)
		assert.Eq(t, []string{"mcp_a", "mcp_b"}, rec.LoadedTools)
		assert.Eq(t, true, strings.Contains(string(rec.Plan), "ship T08"))
		assert.Eq(t, []string{"mcp_a", "mcp_b"}, loadedToolNames(model.Meta))
	})
	t.Run("summary call is a model event of the new session", func(t *testing.T) {
		assert.Eq(t, 1, len(eventsOf(t, repo, "child-1", store.EventModel)))
		assert.Eq(t, 1, len(eventsOf(t, repo, src, store.EventModel)))
	})

	// First turn of the child sees summary + plan; the second still does.
	_, err = runner.Run(ctx, RunRequest{SessionID: "child-1", Prompt: "continue"})
	assert.NoErr(t, err)
	req := prov.reqs[len(prov.reqs)-1].Messages
	assert.Eq(t, 2, len(req))
	assert.Eq(t, "user", req[0].Role)
	assert.Eq(t, true, strings.Contains(req[0].Content, "SUMMARY-main-1"))
	assert.Eq(t, true, strings.Contains(req[0].Content, "ship T08"))
	assert.Eq(t, "continue", req[1].Content)

	_, err = runner.Run(ctx, RunRequest{SessionID: "child-1", Prompt: "again"})
	assert.NoErr(t, err)
	req = prov.reqs[len(prov.reqs)-1].Messages
	assert.Eq(t, true, strings.Contains(req[0].Content, "SUMMARY-main-1"))
	assert.Eq(t, "continue", req[1].Content)
	assert.Eq(t, "again", req[len(req)-1].Content)

	t.Run("compaction in the child folds the handoff in", func(t *testing.T) {
		_, err := runner.Compact(ctx, "child-1")
		assert.NoErr(t, err)
		last := prov.sumReqs[len(prov.sumReqs)-1].Messages[0].Content
		assert.Eq(t, true, strings.Contains(last, "SUMMARY-main-1"))
		_, err = runner.Run(ctx, RunRequest{SessionID: "child-1", Prompt: "after"})
		assert.NoErr(t, err)
		req := prov.reqs[len(prov.reqs)-1].Messages
		assert.Eq(t, 2, len(req))
		assert.Eq(t, false, strings.Contains(req[0].Content, "交接过来"))
	})
}

func TestHandoffReusesCompactionSummary(t *testing.T) {
	repo, runner, prov, src := handoffSource(t)
	ctx := context.Background()
	_, err := runner.Compact(ctx, src)
	assert.NoErr(t, err)
	assert.Eq(t, 1, len(prov.sumReqs))
	beforeEntries := entryCount(t, repo, src)

	model, err := runner.Handoff(ctx, HandoffRequest{SourceID: src})
	assert.NoErr(t, err)
	assert.Eq(t, 1, len(prov.sumReqs)) // no new model call
	assert.Eq(t, beforeEntries, entryCount(t, repo, src))
	assert.Eq(t, 0, len(eventsOf(t, repo, model.ID, store.EventModel)))
	e, err := repo.LatestEntry(ctx, model.ID, store.EntryHandoff)
	assert.NoErr(t, err)
	var rec handoffRecord
	assert.NoErr(t, json.Unmarshal(e.JSON, &rec))
	assert.Eq(t, "SUMMARY-main-1", rec.Summary)

	t.Run("new content after the compaction forces a fresh summary", func(t *testing.T) {
		_, err := runner.Run(ctx, RunRequest{SessionID: src, Prompt: "more work"})
		assert.NoErr(t, err)
		_, err = runner.Handoff(ctx, HandoffRequest{SourceID: src})
		assert.NoErr(t, err)
		assert.Eq(t, 2, len(prov.sumReqs))
		assert.Eq(t, true, strings.Contains(prov.sumReqs[1].Messages[0].Content, "SUMMARY-main-1"))
		assert.Eq(t, true, strings.Contains(prov.sumReqs[1].Messages[0].Content, "more work"))
	})
}

func TestHandoffErrors(t *testing.T) {
	repo, runner, _, src := handoffSource(t)
	ctx := context.Background()
	t.Run("unknown source", func(t *testing.T) {
		_, err := runner.Handoff(ctx, HandoffRequest{SourceID: "nope"})
		assert.Eq(t, true, errorsIs(err, ErrSessionNotFound))
	})
	t.Run("existing target", func(t *testing.T) {
		_, err := runner.Handoff(ctx, HandoffRequest{SourceID: src, NewID: src})
		assert.Eq(t, true, err != nil && strings.Contains(err.Error(), "already exists"))
	})
	t.Run("empty source", func(t *testing.T) {
		empty, err := runner.NewSession(ctx, "empty-src", "t")
		assert.NoErr(t, err)
		_, err = runner.Handoff(ctx, HandoffRequest{SourceID: empty.ID, NewID: "child-empty"})
		assert.Eq(t, true, err != nil)
		_, err = repo.GetSession(ctx, "child-empty")
		assert.Eq(t, true, errorsIs(err, ErrSessionNotFound))
	})
}

// A failed summary must not leave the requested id taken: Gofer retries --from
// with the same --session-id it generated.
func TestHandoffSummaryFailureReleasesID(t *testing.T) {
	repo, runner, prov, src := handoffSource(t)
	ctx := context.Background()
	prov.mu.Lock()
	prov.sumErr = errors.New("summary boom")
	prov.mu.Unlock()
	_, err := runner.Handoff(ctx, HandoffRequest{SourceID: src, NewID: "child-retry"})
	assert.Eq(t, true, err != nil && strings.Contains(err.Error(), "summary boom"))
	_, err = repo.GetSession(ctx, "child-retry")
	assert.Eq(t, true, errorsIs(err, ErrSessionNotFound))

	prov.mu.Lock()
	prov.sumErr = nil
	prov.mu.Unlock()
	got, err := runner.Handoff(ctx, HandoffRequest{SourceID: src, NewID: "child-retry"})
	assert.NoErr(t, err)
	assert.Eq(t, "child-retry", got.ID)
	assert.Eq(t, store.SessionOpen, got.Status)
	assert.Eq(t, 2, entryCount(t, repo, "child-retry")) // segment start + handoff
}

func errorsIs(err, target error) bool { return errors.Is(err, target) }
