package personal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/llm"
)

// segmentText is the content of a request's segment context message ("" when
// the request has none).
func segmentText(req *agentcore.GenerateRequest) string {
	if len(req.Messages) == 0 || !isSegmentContext(req.Messages[0]) {
		return ""
	}
	return req.Messages[0].Content
}

// countUpdates counts the replayed context update messages.
func countUpdates(msgs []agentcore.Message) int {
	n := 0
	for _, m := range msgs {
		if isContextUpdate(m) {
			n++
		}
	}
	return n
}

// updateTexts joins the context update messages' contents.
func updateTexts(msgs []agentcore.Message) string {
	var parts []string
	for _, m := range msgs {
		if isContextUpdate(m) {
			parts = append(parts, m.Content)
		}
	}
	return strings.Join(parts, "\n---\n")
}

func contextUpdateEntries(t *testing.T, repo *store.Store, sessionID string) []store.Entry {
	t.Helper()
	es, err := repo.ListEntries(context.Background(), sessionID, store.EntryFilter{Types: []string{store.EntryContextUpdate}})
	assert.NoErr(t, err)
	return es
}

// effectiveOf is the effective segment context of a session as the next turn
// would see it (adopted records and updates only).
func effectiveOf(t *testing.T, r *Runner, sessionID string) *segmentContext {
	t.Helper()
	model, err := r.LoadSession(context.Background(), sessionID)
	assert.Require(t, assert.NoErr(t, err))
	view, err := r.loadHistory(context.Background(), sessionID)
	assert.Require(t, assert.NoErr(t, err))
	r.resolveSegment(&view, model.Meta)
	return view.eff
}

// ruleFile is a RuleSource whose content can be changed between turns.
type ruleFile struct{ text string }

func (f *ruleFile) source() (string, string, error) {
	sum := sha256Hex(f.text)
	return f.text, sum, nil
}

func sha256Hex(s string) string {
	c := segmentContext{Rules: s}
	c.seal()
	return c.Hash
}

// A failed turn's context update is not adopted: the next turn re-evaluates
// against the adopted state, appends it again and from then on it is
// replayed once - never lost, never duplicated.
func TestContextUpdateFailedTurnRecomputed(t *testing.T) {
	repo := openObserveRepo(t)
	rules := &ruleFile{text: "RULE-V1"}
	fail := false
	prov := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		if fail {
			return nil, errors.New("provider exploded")
		}
		return finalText("ok")
	}}
	runner := compactRunner(t, repo, prov, nil, func(c *Config) { c.RuleSource = rules.source })
	ctx := context.Background()
	first, err := runner.Run(ctx, RunRequest{Prompt: "one"})
	assert.NoErr(t, err)
	sid := first.SessionID

	rules.text = "RULE-V2"
	fail = true
	bad, err := runner.Run(ctx, RunRequest{SessionID: sid, Prompt: "doomed"})
	assert.Eq(t, true, err != nil)
	assert.Eq(t, RunFailed, bad.State)
	// The failed turn did see the update...
	assert.StrContains(t, updateTexts(prov.reqs[len(prov.reqs)-1].Messages), "RULE-V2")
	// ...but it is not adopted: the effective rules are still V1.
	assert.Eq(t, "RULE-V1", effectiveOf(t, runner, sid).Rules)

	fail = false
	for _, prompt := range []string{"two", "three"} {
		_, err = runner.Run(ctx, RunRequest{SessionID: sid, Prompt: prompt})
		assert.NoErr(t, err)
		req := prov.reqs[len(prov.reqs)-1]
		assert.Eq(t, 1, countUpdates(req.Messages))
		assert.StrContains(t, updateTexts(req.Messages), "RULE-V2")
		assert.StrContains(t, segmentText(req), "RULE-V1") // the snapshot is frozen
	}
	es := contextUpdateEntries(t, repo, sid)
	assert.Eq(t, 2, len(es)) // the failed turn's and the adopted one
	assert.Eq(t, bad.TurnID, es[0].TurnID)
	assert.Eq(t, "RULE-V2", effectiveOf(t, runner, sid).Rules)

	t.Run("summary input skips context updates", func(t *testing.T) {
		_, err := runner.Compact(ctx, sid)
		assert.NoErr(t, err)
		sum := prov.sumReqs[len(prov.sumReqs)-1].Messages[0].Content
		assert.NotContains(t, sum, "RULE-V2")
		assert.NotContains(t, sum, contextUpdateIntro)
		// The new segment re-read the rules; the update is superseded.
		_, err = runner.Run(ctx, RunRequest{SessionID: sid, Prompt: "after break"})
		assert.NoErr(t, err)
		req := prov.reqs[len(prov.reqs)-1]
		assert.Eq(t, 0, countUpdates(req.Messages))
		assert.StrContains(t, segmentText(req), "RULE-V2")
	})
}

// A mid-turn segment break supersedes this turn's context update in memory
// exactly like loadHistory does on the next turn.
func TestContextUpdateSupersededByMidTurnBreak(t *testing.T) {
	repo := openObserveRepo(t)
	rules := &ruleFile{text: "RULE-A"}
	var prov *scriptProv
	prov = &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		if len(prov.sumReqs) == 0 && n > 1 {
			return callTool(fmt.Sprintf("c%d", n))
		}
		return finalText("done")
	}}
	runner := compactRunner(t, repo, prov, bigToolRegistry(2000, nil), func(c *Config) { c.RuleSource = rules.source })
	ctx := context.Background()
	first, err := runner.Run(ctx, RunRequest{Prompt: "one"})
	assert.NoErr(t, err)
	rules.text = "RULE-B"
	_, err = runner.Run(ctx, RunRequest{SessionID: first.SessionID, Prompt: "long job"})
	assert.NoErr(t, err)
	assert.Require(t, assert.True(t, len(prov.sumReqs) >= 1))
	last := prov.reqs[len(prov.reqs)-1]
	assert.Eq(t, 0, countUpdates(last.Messages))
	assert.StrContains(t, segmentText(last), "RULE-B")

	_, err = runner.Run(ctx, RunRequest{SessionID: first.SessionID, Prompt: "next"})
	assert.NoErr(t, err)
	next := prov.reqs[len(prov.reqs)-1]
	assert.Eq(t, 0, countUpdates(next.Messages))
	assert.Eq(t, segmentText(last), segmentText(next))
	assert.Eq(t, 1, len(contextUpdateEntries(t, repo, first.SessionID)))

	// A record whose kept tail still contains an earlier update supersedes it:
	// the update is neither replayed (it would contradict the newer snapshot
	// that comes before it) nor applied.
	t.Run("kept but superseded update", func(t *testing.T) {
		sid := "superseded"
		_, err := runner.NewSession(ctx, sid, "t")
		assert.NoErr(t, err)
		rules.text = "RULE-C"
		_, err = runner.Run(ctx, RunRequest{SessionID: sid, Prompt: "short"})
		assert.NoErr(t, err)
		es := contextUpdateEntries(t, repo, sid)
		assert.Require(t, assert.Eq(t, 1, len(es)))
		seg := &segmentContext{Rules: "RULE-D", RulesHash: "d-hash"}
		seg.seal()
		raw, _ := json.Marshal(compactionRecord{Summary: "S", FirstKeptSeq: es[0].Seq, Reason: segmentReasonManual, Context: seg})
		_, err = repo.AppendEntries(ctx, sid, store.NewEntry{Type: store.EntryCompaction, JSON: raw})
		assert.NoErr(t, err)
		view, err := runner.loadHistory(ctx, sid)
		assert.NoErr(t, err)
		assert.Eq(t, 0, countUpdates(messagesOf(view.items)))
		assert.True(t, len(view.items) > 0) // the turn's messages are kept
		assert.Eq(t, "RULE-D", effectiveOf(t, runner, sid).Rules)
	})
}

func messagesOf(items []ctxItem) []agentcore.Message {
	out := make([]agentcore.Message, len(items))
	for i, it := range items {
		out[i] = it.msg
	}
	return out
}

// The segment context message renders the snapshot in the fixed order 环境 →
// 项目规则 → 记忆索引 → skills → 附加提示 → 延迟工具列表 → 交接包, as one user
// message carrying the marker.
func TestSegmentContextMessageOrder(t *testing.T) {
	seg := &segmentContext{
		Environment: "# Environment\n- OS: x", Rules: "# /w/AGENTS.md\nRULES", MemoryIndex: renderMemorySection("MEM"),
		Skills: "# Skills\n- s — d", AppendPrompt: "APPEND", DeferredTools: "## 延迟加载的工具\n- mcp_x — y",
	}
	msg, ok := segmentContextMessage(seg, "PKG", false)
	assert.Require(t, assert.True(t, ok))
	assert.Eq(t, "user", msg.Role)
	assert.True(t, isSegmentContext(msg))
	assert.False(t, isContextUpdate(msg))
	assert.True(t, strings.HasPrefix(msg.Content, segmentContextIntro))
	order := []string{"# Environment", rulesHeading, "RULES", "## 记忆索引", "# Skills", appendHeading, "## 延迟加载的工具", summaryMessage("PKG").Content}
	at := -1
	for _, part := range order {
		i := strings.Index(msg.Content, part)
		assert.True(t, i > at, part)
		at = i
	}
	assert.True(t, strings.HasSuffix(msg.Content, summaryMessage("PKG").Content))

	t.Run("empty parts are skipped", func(t *testing.T) {
		msg, ok := segmentContextMessage(&segmentContext{Rules: "R"}, "", false)
		assert.True(t, ok)
		assert.Eq(t, segmentContextIntro+"\n\n"+rulesHeading+"\n\nR", msg.Content)
		_, ok = segmentContextMessage(&segmentContext{}, "", false)
		assert.False(t, ok)
	})
	t.Run("update message", func(t *testing.T) {
		empty, rules := "", "NEW-RULES"
		m := contextUpdateRecord{Rules: &rules, AppendPrompt: &empty}.message()
		assert.Eq(t, "user", m.Role)
		assert.True(t, isContextUpdate(m))
		assert.False(t, isSegmentContext(m))
		assert.StrContains(t, m.Content, "NEW-RULES")
		assert.StrContains(t, m.Content, "附加提示已清除")
		assert.NotContains(t, m.Content, "# Environment")
	})
}

// The hook context (RunRequest.SystemContext, also a Stop-wait reply) is a
// user message of its turn: no request ever carries a role=system message.
func TestHookContextIsUserRole(t *testing.T) {
	repo := openObserveRepo(t)
	rules := &ruleFile{text: "R1"}
	prov := &scriptProv{name: "main", normal: func(int, *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		return finalText("ok")
	}}
	runner := compactRunner(t, repo, prov, nil, func(c *Config) { c.RuleSource = rules.source })
	ctx := context.Background()
	first, err := runner.Run(ctx, RunRequest{Prompt: "one"})
	assert.NoErr(t, err)
	rules.text = "R2"
	_, err = runner.Run(ctx, RunRequest{SessionID: first.SessionID, Prompt: "two", SystemContext: "HOOK-CTX"})
	assert.NoErr(t, err)
	_, err = runner.Run(ctx, RunRequest{SessionID: first.SessionID, Prompt: "three"})
	assert.NoErr(t, err)
	for _, req := range prov.reqs {
		for _, m := range req.Messages {
			assert.NotEq(t, "system", m.Role)
		}
	}
	msgs := prov.reqs[1].Messages
	n := len(msgs)
	assert.Eq(t, agentcore.Message{Role: "user", Content: "HOOK-CTX"}, msgs[n-2])
	assert.Eq(t, "two", msgs[n-1].Content)
	assert.True(t, isContextUpdate(msgs[n-3])) // update, hook context, prompt
	// Turn-only: the next turn no longer carries it.
	assert.NotContains(t, updateTexts(prov.reqs[2].Messages)+segmentText(prov.reqs[2]), "HOOK-CTX")
}

// A legacy session (no segment record, only meta) renders from meta; a rule
// change appends an update and meta is not touched by it.
func TestContextUpdateLegacySession(t *testing.T) {
	repo := openObserveRepo(t)
	rules := &ruleFile{text: "NEW-RULES"}
	prov := &scriptProv{name: "main", normal: func(int, *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		return finalText("ok")
	}}
	runner := compactRunner(t, repo, prov, nil, func(c *Config) { c.RuleSource = rules.source })
	ctx := context.Background()
	meta := map[string]any{"personal_rules_content": "OLD-RULES", "personal_rules_hash": "old-hash"}
	assert.NoErr(t, repo.CreateSession(ctx, Session{ID: "legacy-cu", Title: "t", Status: store.SessionOpen, Meta: meta}))
	for range 2 {
		_, err := runner.Run(ctx, RunRequest{SessionID: "legacy-cu", Prompt: "q"})
		assert.NoErr(t, err)
		req := prov.reqs[len(prov.reqs)-1]
		assert.StrContains(t, segmentText(req), "OLD-RULES")
		assert.Eq(t, 1, countUpdates(req.Messages))
		assert.StrContains(t, updateTexts(req.Messages), "NEW-RULES")
	}
	stored, err := repo.GetSession(ctx, "legacy-cu")
	assert.NoErr(t, err)
	assert.Eq(t, "OLD-RULES", stored.Meta["personal_rules_content"])
}

// generateOnly hides a provider's streaming method so the runtime calls
// Generate (the fake server below answers plain JSON).
type generateOnly struct{ p agentcore.Provider }

func (g generateOnly) Name() string { return g.p.Name() }
func (g generateOnly) Generate(ctx context.Context, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	return g.p.Generate(ctx, req)
}

// On the Claude path role=system messages are merged into the top-level
// system, so before T6 a rule update or hook context changed it inside a
// segment. Now the request body's system is the same bytes for every turn
// (with and without prompt caching) and the update/hook context are user
// messages.
func TestClaudeSystemStableInSegment(t *testing.T) {
	for _, cache := range []bool{false, true} {
		t.Run(fmt.Sprintf("prompt_cache=%v", cache), func(t *testing.T) {
			var mu sync.Mutex
			var bodies []map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				var body map[string]any
				_ = json.Unmarshal(raw, &body)
				mu.Lock()
				bodies = append(bodies, body)
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":1}}`))
			}))
			t.Cleanup(srv.Close)
			claude := llm.NewClaudeProvider(llm.ClaudeConfig{APIKey: "k", BaseURL: srv.URL, PromptCache: cache})
			repo := openObserveRepo(t)
			rules := &ruleFile{text: "RULE-ONE"}
			runner, err := New(Config{Repository: repo, Provider: generateOnly{claude}, Model: "m", ProviderName: "p",
				PromptBuilder: staticTestPrompt("BASE"), RuleSource: rules.source,
				EnvironmentSource: func() string { return "# Environment\n- Workspace: /w" }})
			assert.Require(t, assert.NoErr(t, err))
			ctx := context.Background()
			first, err := runner.Run(ctx, RunRequest{Prompt: "one"})
			assert.Require(t, assert.NoErr(t, err))
			rules.text = "RULE-TWO"
			_, err = runner.Run(ctx, RunRequest{SessionID: first.SessionID, Prompt: "two", SystemContext: "HOOK-CTX"})
			assert.Require(t, assert.NoErr(t, err))
			_, err = runner.Run(ctx, RunRequest{SessionID: first.SessionID, Prompt: "three"})
			assert.Require(t, assert.NoErr(t, err))

			assert.Require(t, assert.Len(t, bodies, 3))
			sys0, _ := json.Marshal(bodies[0]["system"])
			assert.StrContains(t, string(sys0), "BASE")
			for _, body := range bodies {
				sys, _ := json.Marshal(body["system"])
				assert.Eq(t, string(sys0), string(sys))
				raw, _ := json.Marshal(body["messages"])
				assert.NotContains(t, string(raw), `"role":"system"`)
			}
			second, _ := json.Marshal(bodies[1]["messages"])
			assert.StrContains(t, string(second), "HOOK-CTX")
			assert.StrContains(t, string(second), "RULE-TWO")
			assert.NotContains(t, string(sys0), "RULE-")
		})
	}
}
