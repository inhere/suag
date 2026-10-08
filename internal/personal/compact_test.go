package personal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/pkg/agentcore"
)

// Small window so a few KB of text crosses the threshold:
// threshold = 3000-500 = 2500 tokens (~10000 chars); keep ~800 tokens (~3200 chars).
const (
	testWindow  = 3000
	testReserve = 500
	testKeep    = 800
)

// scriptProv answers summary requests itself and delegates ordinary model
// calls to normal(n, req) with n counting only ordinary calls.
type scriptProv struct {
	mu      sync.Mutex
	name    string
	normal  func(n int, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error)
	reqs    []*agentcore.GenerateRequest // ordinary calls
	sumReqs []*agentcore.GenerateRequest // summary calls
}

func (p *scriptProv) Name() string { return p.name }

func (p *scriptProv) Generate(_ context.Context, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	cp := *req
	cp.Messages = append([]agentcore.Message(nil), req.Messages...)
	p.mu.Lock()
	if req.System == compactSystemPrompt {
		p.sumReqs = append(p.sumReqs, &cp)
		n := len(p.sumReqs)
		p.mu.Unlock()
		return &agentcore.GenerateResult{Content: fmt.Sprintf("SUMMARY-%s-%d", p.name, n), FinishReason: agentcore.FinishFinal}, nil
	}
	p.reqs = append(p.reqs, &cp)
	n := len(p.reqs)
	p.mu.Unlock()
	return p.normal(n, &cp)
}

func finalText(s string) (*agentcore.GenerateResult, error) {
	return &agentcore.GenerateResult{Content: s, FinishReason: agentcore.FinishFinal}, nil
}

func callTool(id string) (*agentcore.GenerateResult, error) {
	return &agentcore.GenerateResult{
		ToolCalls:    []agentcore.ToolCall{{CallID: id, Name: "big", Arguments: map[string]any{}}},
		FinishReason: agentcore.FinishToolCalls,
	}, nil
}

func bigToolRegistry(size int, onCall func(int)) agentcore.ToolRegistry {
	reg := tools.NewRegistry()
	var mu sync.Mutex
	n := 0
	reg.Register(fnTool{name: "big", fn: func() (*agentcore.ToolResult, error) {
		mu.Lock()
		n++
		i := n
		mu.Unlock()
		if onCall != nil {
			onCall(i)
		}
		return &agentcore.ToolResult{Name: "big", Content: strings.Repeat("x", size)}, nil
	}})
	return reg
}

func compactRunner(t *testing.T, repo *store.Store, prov agentcore.Provider, reg agentcore.ToolRegistry, mut func(*Config)) *Runner {
	t.Helper()
	cfg := Config{
		Repository: repo, Provider: prov, Tools: reg, Model: "m", ProviderName: "p", MaxSteps: 80,
		ContextWindow: testWindow, CompactReserveTokens: testReserve, CompactKeepTokens: testKeep,
	}
	if mut != nil {
		mut(&cfg)
	}
	r, err := New(cfg)
	assert.NoErr(t, err)
	return r
}

// assertWellFormed checks a request's messages: first is user (or a leading
// system notice then user) and every call is directly followed by its result.
func assertWellFormed(t *testing.T, msgs []agentcore.Message) {
	t.Helper()
	assert.Eq(t, true, len(msgs) > 0)
	first := 0
	for first < len(msgs) && msgs[first].Role == "system" {
		first++
	}
	assert.Eq(t, "user", msgs[first].Role)
	for i, m := range msgs {
		switch {
		case m.Role == "tool":
			assert.Eq(t, true, i > 0 && msgs[i-1].Role == "assistant" && msgs[i-1].ToolCall != nil && m.ToolCall != nil && msgs[i-1].ToolCall.CallID == m.ToolCall.CallID)
		case m.Role == "assistant" && m.ToolCall != nil:
			assert.Eq(t, true, i+1 < len(msgs) && msgs[i+1].Role == "tool" && msgs[i+1].ToolCall.CallID == m.ToolCall.CallID)
		}
	}
}

func compactionEntries(t *testing.T, repo *store.Store, sessionID string) []compactionRecord {
	t.Helper()
	es, err := repo.ListEntries(context.Background(), sessionID, store.EntryFilter{Types: []string{store.EntryCompaction}})
	assert.NoErr(t, err)
	var out []compactionRecord
	for _, e := range es {
		var rec compactionRecord
		assert.NoErr(t, json.Unmarshal(e.JSON, &rec))
		out = append(out, rec)
	}
	return out
}

func TestCompactAcrossTurnsExceedingThreshold(t *testing.T) {
	repo := openObserveRepo(t)
	prov := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		return finalText(fmt.Sprintf("answer-%d %s", n, strings.Repeat("a", 800)))
	}}
	runner := compactRunner(t, repo, prov, nil, nil)
	sid := ""
	for i := 1; i <= 14; i++ {
		res, err := runner.Run(context.Background(), RunRequest{SessionID: sid, Prompt: fmt.Sprintf("question-%d %s", i, strings.Repeat("q", 800))})
		assert.NoErr(t, err)
		sid = res.SessionID
	}
	assert.Eq(t, true, len(prov.sumReqs) >= 1)
	recs := compactionEntries(t, repo, sid)
	assert.Eq(t, true, len(recs) >= 1)
	for _, rec := range recs {
		assert.Eq(t, true, rec.TokensAfter < rec.TokensBefore && rec.FirstKeptSeq > 0)
	}
	evs := eventsOf(t, repo, sid, store.EventCompaction)
	assert.Eq(t, len(recs), len(evs))
	assert.Eq(t, "ok", evs[0].Status)
	// Every ordinary request fits the window and is well formed; the latest holds
	// the summary as first message plus the newest turn.
	for _, req := range prov.reqs {
		assertWellFormed(t, req.Messages)
		assert.Eq(t, true, charsOfMessages(req.Messages)/4 <= testWindow)
	}
	last := prov.reqs[len(prov.reqs)-1]
	assert.Eq(t, true, strings.Contains(last.Messages[0].Content, "SUMMARY-main-"))
	assert.Eq(t, true, strings.Contains(last.Messages[len(last.Messages)-1].Content, "question-14"))
}

func TestCompactInsideSingleLongTurn(t *testing.T) {
	repo := openObserveRepo(t)
	prov := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		if n <= 40 {
			return callTool(fmt.Sprintf("c%d", n))
		}
		return finalText("done")
	}}
	runner := compactRunner(t, repo, prov, bigToolRegistry(2000, nil), nil)
	res, err := runner.Run(context.Background(), RunRequest{Prompt: "do the long job"})
	assert.NoErr(t, err)
	assert.Eq(t, "done", res.Output)
	assert.Eq(t, 41, len(prov.reqs))
	assert.Eq(t, true, len(prov.sumReqs) >= 2) // repeated compactions in one turn
	for _, req := range prov.reqs {
		assertWellFormed(t, req.Messages)
		assert.Eq(t, true, charsOfMessages(req.Messages)/4 <= testWindow)
	}
	// All of the turn's messages stay persisted in full.
	turnMsgs, err := repo.AgentStorage().LoadMessages(context.Background(), res.TurnID)
	assert.NoErr(t, err)
	assert.Eq(t, 1+80+1, len(turnMsgs))
	// The cut maps to a persisted message entry of this turn.
	recs := compactionEntries(t, repo, res.SessionID)
	assert.Eq(t, true, len(recs) >= 2 && recs[1].FirstKeptSeq > recs[0].FirstKeptSeq)
}

func TestCompactHugeToolResultTruncatesOrFails(t *testing.T) {
	t.Run("truncated and continues", func(t *testing.T) {
		repo := openObserveRepo(t)
		prov := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
			if n == 1 {
				return callTool("c1")
			}
			return finalText("ok")
		}}
		runner := compactRunner(t, repo, prov, bigToolRegistry(60000, nil), nil)
		res, err := runner.Run(context.Background(), RunRequest{Prompt: "read it"})
		assert.NoErr(t, err)
		assert.Eq(t, "ok", res.Output)
		second := prov.reqs[1].Messages
		toolMsg := second[len(second)-1]
		assert.Eq(t, "tool", toolMsg.Role)
		assert.Eq(t, true, len(toolMsg.Content) <= 8200 && strings.Contains(toolMsg.Content, "truncated"))
		assert.Eq(t, true, len(toolMsg.ToolResult.Content) <= 8200)
		assert.Eq(t, 0, len(prov.sumReqs)) // nothing cuttable
		evs := eventsOf(t, repo, res.SessionID, store.EventCompaction)
		assert.Eq(t, 1, len(evs))
		assert.Eq(t, "error", evs[0].Status)
		// The original is kept in full.
		msgs, err := repo.AgentStorage().LoadMessages(context.Background(), res.TurnID)
		assert.NoErr(t, err)
		assert.Eq(t, 60000, len(msgs[2].Content))
	})
	t.Run("still too big fails the turn", func(t *testing.T) {
		repo := openObserveRepo(t)
		prov := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
			if n == 1 {
				return callTool("c1")
			}
			return finalText("never")
		}}
		runner := compactRunner(t, repo, prov, bigToolRegistry(60000, nil), func(c *Config) {
			c.ContextWindow, c.CompactReserveTokens = 600, 100 // threshold 500 tokens < 2000 chars floor
		})
		res, err := runner.Run(context.Background(), RunRequest{Prompt: "read it"})
		assert.Eq(t, true, errors.Is(err, ErrContextOverflow))
		assert.Eq(t, RunFailed, res.State)
		assert.Eq(t, 1, len(prov.reqs)) // no retry loop
	})
}

func TestPickCutNeverSplitsCallAndResult(t *testing.T) {
	user := func(s string) ctxItem { return ctxItem{msg: agentcore.Message{Role: "user", Content: s}} }
	call := func(id string) ctxItem {
		return ctxItem{msg: agentcore.Message{Role: "assistant", ToolCall: &agentcore.ToolCall{CallID: id, Name: "t"}}}
	}
	result := func(id string) ctxItem {
		return ctxItem{msg: agentcore.Message{Role: "tool", Content: strings.Repeat("r", 400),
			ToolCall: &agentcore.ToolCall{CallID: id}, ToolResult: &agentcore.ToolResult{CallID: id, Content: strings.Repeat("r", 400)}}}
	}
	final := ctxItem{msg: agentcore.Message{Role: "assistant", Content: "fin"}}
	// user, A1,T1, A2,T2 (parallel calls of one response), final, user
	items := []ctxItem{user("u1"), call("1"), result("1"), call("2"), result("2"), final, user("u2")}
	t.Run("legal cut positions", func(t *testing.T) {
		legal := map[int]bool{}
		for i := 1; i < len(items); i++ {
			if validCut(items, i) {
				legal[i] = true
			}
		}
		// before u2(6), before final(5), between T1 and A2(3); never before T(2,4) or A1/A2 after a user(1).
		assert.Eq(t, map[int]bool{3: true, 5: true, 6: true}, legal)
	})
	t.Run("aligned cut is legal for every keep budget", func(t *testing.T) {
		for keep := 1; keep < 600; keep += 7 {
			cut := pickCut(items, keep, false)
			if cut == 0 {
				continue
			}
			assert.Eq(t, true, validCut(items, cut))
			kept := items[cut:]
			// the kept part starts at a user or at an assistant message, never a dangling result
			assert.Eq(t, true, kept[0].msg.Role != "tool")
		}
	})
	t.Run("end cut only after a closed pair or final answer", func(t *testing.T) {
		assert.Eq(t, 7, pickCut(items[:6], 0, true)+1)
		assert.Eq(t, 0, pickCut([]ctxItem{user("a"), user("b")}, 0, true))
	})
}

func TestCompactedTurnCancelledIsNotAdopted(t *testing.T) {
	for _, mode := range []string{"cancelled", "failed"} {
		t.Run(mode, func(t *testing.T) {
			repo := openObserveRepo(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var prov *scriptProv
			turn2 := false
			prov = &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
				if !turn2 {
					return finalText("first answer " + strings.Repeat("f", 100))
				}
				// Doomed turn: keep calling the tool until a compaction happened,
				// then die in the requested way.
				if len(prov.sumReqs) > 0 {
					if mode == "failed" {
						return nil, errors.New("provider exploded")
					}
					cancel()
				}
				return callTool(fmt.Sprintf("c%d", n))
			}}
			runner := compactRunner(t, repo, prov, bigToolRegistry(2000, nil), nil)
			first, err := runner.Run(context.Background(), RunRequest{Prompt: "first question"})
			assert.NoErr(t, err)
			turn2 = true
			bad, err := runner.Run(ctx, RunRequest{SessionID: first.SessionID, Prompt: "doomed question"})
			assert.Eq(t, true, err != nil)
			want := RunFailed
			if mode == "cancelled" {
				want = RunCanceled
			}
			assert.Eq(t, want, bad.State)
			// The doomed turn did write a compaction entry ...
			es, err := repo.ListEntries(context.Background(), first.SessionID, store.EntryFilter{TurnID: bad.TurnID, Types: []string{store.EntryCompaction}})
			assert.NoErr(t, err)
			assert.Eq(t, true, len(es) >= 1)

			// ... which the next turn must not use, nor the doomed turn's messages.
			turn2 = false
			_, err = runner.Run(context.Background(), RunRequest{SessionID: first.SessionID, Prompt: "third question"})
			assert.NoErr(t, err)
			req := prov.reqs[len(prov.reqs)-1]
			var texts []string
			for _, m := range req.Messages {
				texts = append(texts, m.Role+":"+m.Content)
			}
			joined := strings.Join(texts, "\n")
			assert.Eq(t, false, strings.Contains(joined, "SUMMARY-"))
			assert.Eq(t, false, strings.Contains(joined, "doomed question"))
			assert.Eq(t, true, strings.Contains(joined, "first question") && strings.Contains(joined, "first answer"))
			assertWellFormed(t, req.Messages)
		})
	}
}

func TestCompactModelConfigured(t *testing.T) {
	run := func(t *testing.T, withCompact bool) (main, comp *scriptProv, repo *store.Store, sid string) {
		repo = openObserveRepo(t)
		main = &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
			return finalText(strings.Repeat("a", 900))
		}}
		comp = &scriptProv{name: "compactor", normal: func(int, *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
			return nil, errors.New("compact provider must only summarize")
		}}
		runner := compactRunner(t, repo, main, nil, func(c *Config) {
			if withCompact {
				c.CompactProvider, c.CompactModel = comp, "small-model"
			}
		})
		for i := 0; i < 12; i++ {
			res, err := runner.Run(context.Background(), RunRequest{SessionID: sid, Prompt: strings.Repeat("q", 900)})
			assert.NoErr(t, err)
			sid = res.SessionID
		}
		return
	}
	t.Run("configured uses compact provider", func(t *testing.T) {
		main, comp, repo, sid := run(t, true)
		assert.Eq(t, 0, len(main.sumReqs))
		assert.Eq(t, true, len(comp.sumReqs) >= 1)
		assert.Eq(t, true, strings.Contains(main.reqs[len(main.reqs)-1].Messages[0].Content, "SUMMARY-compactor-"))
		// The summary call is observed: a model event carries the compact model.
		found := false
		for _, e := range eventsOf(t, repo, sid, store.EventModel) {
			found = found || e.Attrs["model"] == "small-model"
		}
		assert.Eq(t, true, found)
	})
	t.Run("not configured uses the turn provider", func(t *testing.T) {
		main, comp, _, _ := run(t, false)
		assert.Eq(t, 0, len(comp.sumReqs))
		assert.Eq(t, true, len(main.sumReqs) >= 1)
	})
}

func TestCompactResumeRebuildsSameView(t *testing.T) {
	repo := openObserveRepo(t)
	prov := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		if n <= 30 {
			return callTool(fmt.Sprintf("c%d", n))
		}
		return finalText("long job done")
	}}
	runner := compactRunner(t, repo, prov, bigToolRegistry(2000, nil), nil)
	res, err := runner.Run(context.Background(), RunRequest{Prompt: "long job"})
	assert.NoErr(t, err)
	assert.Eq(t, true, len(compactionEntries(t, repo, res.SessionID)) >= 1)
	lastView := prov.reqs[len(prov.reqs)-1].Messages // the view of the final model call

	// A brand-new Runner (resume) must rebuild: that view + final answer + new prompt.
	fresh := compactRunner(t, repo, prov, bigToolRegistry(2000, nil), nil)
	prov.normal = func(int, *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) { return finalText("next") }
	_, err = fresh.Run(context.Background(), RunRequest{SessionID: res.SessionID, Prompt: "continue"})
	assert.NoErr(t, err)
	got := prov.reqs[len(prov.reqs)-1].Messages
	want := append(append([]agentcore.Message(nil), lastView...),
		agentcore.Message{Role: "assistant", Content: "long job done"},
		agentcore.Message{Role: "user", Content: "continue"})
	assert.Eq(t, len(want), len(got))
	for i := range want {
		assert.Eq(t, want[i].Role, got[i].Role)
		assert.Eq(t, want[i].Content, got[i].Content)
	}
	assertWellFormed(t, got)
}

func TestManualCompactAndPlanInSummary(t *testing.T) {
	repo := openObserveRepo(t)
	prov := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		return finalText(fmt.Sprintf("reply-%d", n))
	}}
	runner := compactRunner(t, repo, prov, nil, func(c *Config) { c.ContextWindow, c.CompactReserveTokens = 100000, 1000 })
	t.Run("empty session has nothing to compact", func(t *testing.T) {
		model, err := runner.NewSession(context.Background(), "empty-one", "t")
		assert.NoErr(t, err)
		_, err = runner.Compact(context.Background(), model.ID)
		assert.Eq(t, true, err != nil)
	})
	res, err := runner.Run(context.Background(), RunRequest{Prompt: "hello there " + strings.Repeat("h", 2000)})
	assert.NoErr(t, err)
	_, err = repo.AppendEntries(context.Background(), res.SessionID, store.NewEntry{Type: store.EntryPlan, JSON: json.RawMessage(`{"plan":[{"step":"ship T06","status":"in_progress"}]}`)})
	assert.NoErr(t, err)

	out, err := runner.Compact(context.Background(), res.SessionID)
	assert.NoErr(t, err)
	assert.Eq(t, true, out.TokensAfter < out.TokensBefore)
	assert.Eq(t, 1, len(prov.sumReqs))
	assert.Eq(t, true, strings.Contains(prov.sumReqs[0].Messages[0].Content, "ship T06"))
	assert.Eq(t, true, strings.Contains(prov.sumReqs[0].Messages[0].Content, "hello there"))

	_, err = runner.Run(context.Background(), RunRequest{SessionID: res.SessionID, Prompt: "after compact"})
	assert.NoErr(t, err)
	msgs := prov.reqs[len(prov.reqs)-1].Messages
	assert.Eq(t, 2, len(msgs))
	assert.Eq(t, true, strings.Contains(msgs[0].Content, "SUMMARY-main-1"))
	assert.Eq(t, "after compact", msgs[1].Content)

	// A second compact folds the earlier summary in.
	_, err = runner.Compact(context.Background(), res.SessionID)
	assert.NoErr(t, err)
	assert.Eq(t, true, strings.Contains(prov.sumReqs[1].Messages[0].Content, "SUMMARY-main-1"))
}

func TestTruncateMiddleKeepsUTF8(t *testing.T) {
	s := strings.Repeat("中文字符", 3000)
	out := truncateMiddle(s, 2001)
	assert.Eq(t, true, len(out) < 2200)
	for _, r := range out {
		assert.Eq(t, true, r != '�')
	}
	assert.Eq(t, "short", truncateMiddle("short", 100))
}
