package personal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/memory"
)

// memorySource is a Config.MemoryIndex whose index text can be changed
// between calls; it counts the calls.
type memorySource struct {
	mu    sync.Mutex
	text  string
	err   error
	calls int
}

func (s *memorySource) set(text string, err error) {
	s.mu.Lock()
	s.text, s.err = text, err
	s.mu.Unlock()
}

func (s *memorySource) index(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return s.text, s.err
}

func (s *memorySource) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func memoryLine(title string) string { return "### Memory: user\n- note (user): " + title + "\n" }

func newMemoryRunner(t *testing.T, repo *store.Store, prov agentcore.Provider, src *memorySource) *Runner {
	t.Helper()
	cfg := Config{Repository: repo, Provider: prov, Model: "m", ProviderName: "p",
		RuleSource: func() (string, string, error) { return "RULES-TEXT", "rules-hash", nil }}
	if src != nil {
		cfg.MemoryIndex = src.index
	}
	r, err := New(cfg)
	assert.Require(t, assert.NoErr(t, err))
	return r
}

func TestMemorySnapshotFrozenPerSession(t *testing.T) {
	ctx := context.Background()
	repo := openObserveRepo(t)
	prov := &rulesProvider{}
	src := &memorySource{text: memoryLine("first-title")}
	runner := newMemoryRunner(t, repo, prov, src)

	t.Run("new session prompt has the index after the rules", func(t *testing.T) {
		model, err := runner.NewSession(ctx, "mem-a", "t")
		assert.Require(t, assert.NoErr(t, err))
		_, err = runner.Run(ctx, RunRequest{SessionID: model.ID, Prompt: "one"})
		assert.Require(t, assert.NoErr(t, err))
		system := prov.requests[0].System
		assert.StrContains(t, system, "## 记忆索引")
		assert.StrContains(t, system, "- note (user): first-title")
		assert.True(t, strings.Index(system, "RULES-TEXT") < strings.Index(system, "## 记忆索引"))
		assert.Eq(t, 1, src.count())
	})
	t.Run("writes in the same session do not change the prompt", func(t *testing.T) {
		src.set(memoryLine("second-title"), nil)
		_, err := runner.Run(ctx, RunRequest{SessionID: "mem-a", Prompt: "two"})
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, prov.requests[0].System, prov.requests[1].System)
		assert.Eq(t, 1, src.count())
	})
	t.Run("turn_context carries the snapshot hash", func(t *testing.T) {
		stored, err := repo.GetSession(ctx, "mem-a")
		assert.Require(t, assert.NoErr(t, err))
		hash := metaString(stored.Meta, metaMemoryIndexHash)
		sum := sha256.Sum256([]byte(metaString(stored.Meta, metaMemoryIndexContent)))
		assert.Eq(t, hex.EncodeToString(sum[:]), hash)
		events := eventsOf(t, repo, "mem-a", store.EventTurnContext)
		assert.Eq(t, 2, len(events))
		for _, ev := range events {
			assert.NotEmpty(t, hash)
			assert.Eq(t, hash, ev.Attrs["memory_hash"])
		}
	})
	t.Run("a new session takes a new snapshot", func(t *testing.T) {
		res, err := runner.Run(ctx, RunRequest{Prompt: "three"})
		assert.Require(t, assert.NoErr(t, err))
		assert.NotEq(t, "mem-a", res.SessionID)
		assert.StrContains(t, prov.requests[2].System, "second-title")
	})
	t.Run("empty memory still explains the tool", func(t *testing.T) {
		assert.StrContains(t, renderMemorySection(""), memoryIndexEmpty)
	})
}

func TestMemorySnapshotBackfillAndFailure(t *testing.T) {
	ctx := context.Background()
	repo := openObserveRepo(t)
	prov := &rulesProvider{}

	t.Run("old session gets a snapshot on its first turn", func(t *testing.T) {
		old := newMemoryRunner(t, repo, prov, nil)
		res, err := old.Run(ctx, RunRequest{Prompt: "before memory"})
		assert.Require(t, assert.NoErr(t, err))
		assert.NotContains(t, prov.requests[0].System, "## 记忆索引")
		stored, _ := repo.GetSession(ctx, res.SessionID)
		_, has := stored.Meta[metaMemoryIndexHash]
		assert.False(t, has)

		src := &memorySource{text: memoryLine("backfilled")}
		runner := newMemoryRunner(t, repo, prov, src)
		_, err = runner.Run(ctx, RunRequest{SessionID: res.SessionID, Prompt: "after"})
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, prov.requests[1].System, "backfilled")
		stored, _ = repo.GetSession(ctx, res.SessionID)
		assert.StrContains(t, metaString(stored.Meta, metaMemoryIndexContent), "backfilled")
		events := eventsOf(t, repo, res.SessionID, store.EventTurnContext)
		assert.Eq(t, "", events[0].Attrs["memory_hash"])
		assert.Eq(t, metaString(stored.Meta, metaMemoryIndexHash), events[1].Attrs["memory_hash"])
		// Only once: the next turn keeps the snapshot.
		_, err = runner.Run(ctx, RunRequest{SessionID: res.SessionID, Prompt: "again"})
		assert.NoErr(t, err)
		assert.Eq(t, 1, src.count())
	})
	t.Run("a failing source does not affect the turn", func(t *testing.T) {
		src := &memorySource{err: errors.New("backend down")}
		runner := newMemoryRunner(t, repo, prov, src)
		res, err := runner.Run(ctx, RunRequest{Prompt: "q"})
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "ok", res.Output)
		system := prov.requests[len(prov.requests)-1].System
		assert.NotContains(t, system, "## 记忆索引")
		assert.StrContains(t, system, "RULES-TEXT")
		stored, _ := repo.GetSession(ctx, res.SessionID)
		_, has := stored.Meta[metaMemoryIndexHash]
		assert.False(t, has)
		// The next turn retries and freezes the snapshot once it works.
		src.set(memoryLine("recovered"), nil)
		_, err = runner.Run(ctx, RunRequest{SessionID: res.SessionID, Prompt: "q2"})
		assert.NoErr(t, err)
		assert.StrContains(t, prov.requests[len(prov.requests)-1].System, "recovered")
	})
	t.Run("a partial index is kept", func(t *testing.T) {
		src := &memorySource{text: memoryLine("partial"), err: errors.New("project scope down")}
		runner := newMemoryRunner(t, repo, prov, src)
		_, err := runner.Run(ctx, RunRequest{Prompt: "q"})
		assert.NoErr(t, err)
		assert.StrContains(t, prov.requests[len(prov.requests)-1].System, "partial")
	})
}

// TestMemorySnapshotRefreshedByCompaction: a compaction re-reads the index
// and the model call of that very step already uses it.
func TestMemorySnapshotRefreshedByCompaction(t *testing.T) {
	repo := openObserveRepo(t)
	src := &memorySource{text: memoryLine("v1")}
	prov := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		if n <= 6 {
			return callTool(fmt.Sprintf("c%d", n))
		}
		return finalText("done")
	}}
	runner := compactRunner(t, repo, prov, bigToolRegistry(2000, func(int) { src.set(memoryLine("v2"), nil) }),
		func(c *Config) { c.MemoryIndex = src.index })
	res, err := runner.Run(context.Background(), RunRequest{Prompt: "long job"})
	assert.Require(t, assert.NoErr(t, err))
	assert.Require(t, assert.True(t, len(prov.sumReqs) >= 1))

	first := -1 // first ordinary request built after a compaction
	for i, req := range prov.reqs {
		if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "SUMMARY-main-") {
			first = i
			break
		}
	}
	assert.Require(t, assert.True(t, first > 0))
	for i, req := range prov.reqs {
		if i < first {
			assert.StrContains(t, req.System, "- note (user): v1")
		} else {
			assert.StrContains(t, req.System, "- note (user): v2")
		}
	}
	stored, err := repo.GetSession(context.Background(), res.SessionID)
	assert.NoErr(t, err)
	assert.StrContains(t, metaString(stored.Meta, metaMemoryIndexContent), "v2")

	t.Run("manual compaction refreshes the stored snapshot", func(t *testing.T) {
		src.set(memoryLine("v3"), nil)
		_, err := runner.Compact(context.Background(), res.SessionID)
		assert.NoErr(t, err)
		stored, _ := repo.GetSession(context.Background(), res.SessionID)
		assert.StrContains(t, metaString(stored.Meta, metaMemoryIndexContent), "v3")
	})
}

func TestMemoryHostHelpers(t *testing.T) {
	ctx := context.Background()
	repo := openObserveRepo(t)
	runner := newMemoryRunner(t, repo, &rulesProvider{}, nil)
	model, err := runner.NewSession(ctx, "mem-h", "t")
	assert.Require(t, assert.NoErr(t, err))
	turn := withObserveScope(ctx, model.ID, "turn-1")

	t.Run("provenance and session from ctx", func(t *testing.T) {
		assert.Eq(t, memory.Provenance{SessionID: "mem-h", TurnID: "turn-1", Source: "tool"}, MemoryProvenance(turn))
		assert.Eq(t, "mem-h", SessionIDFromContext(turn))
		assert.Eq(t, "", SessionIDFromContext(ctx))
	})
	t.Run("taint reads the session meta", func(t *testing.T) {
		assert.False(t, runner.SessionTainted(turn))
		assert.NoErr(t, repo.PatchSessionMeta(ctx, model.ID, map[string]any{metaTaintedAt: "2026-10-11T00:00:00Z"}))
		assert.True(t, runner.SessionTainted(turn))
		// Fail closed outside a turn or for an unknown session.
		assert.True(t, runner.SessionTainted(ctx))
		assert.True(t, runner.SessionTainted(withObserveScope(ctx, "missing", "t")))
	})
	t.Run("memory events", func(t *testing.T) {
		runner.RecordMemoryEvent(turn, memory.Event{Type: memory.EventWrite, Scope: memory.UserScope(), Backend: "files",
			Action: memory.ActionAdd, ID: "a", Kind: memory.KindUser, Source: "tool"})
		runner.RecordMemoryEvent(turn, memory.Event{Type: memory.EventDenied, Scope: memory.UserScope(),
			Action: memory.ActionAdd, Tainted: true, Reason: memory.ReasonDenied, Detail: "no"})
		writes := eventsOf(t, repo, model.ID, memory.EventWrite)
		assert.Eq(t, 1, len(writes))
		assert.Eq(t, "ok", writes[0].Status)
		assert.Eq(t, "a", writes[0].Attrs["id"])
		denied := eventsOf(t, repo, model.ID, memory.EventDenied)
		assert.Eq(t, 1, len(denied))
		assert.Eq(t, "denied", denied[0].Status)
		assert.Eq(t, "no", denied[0].Error)
		assert.Eq(t, true, denied[0].Attrs["tainted"])
	})
}
