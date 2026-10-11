package personal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/mock"
	"github.com/inhere/suag/pkg/agentcore/tools"
)

// metaTool is an fnTool that declares tool metadata.
type metaTool struct {
	fnTool
	meta tools.Meta
}

func (t metaTool) ToolMeta() tools.Meta { return t.meta }

// taintRegistry holds one tool of each taint source. Sizes above the test
// inline threshold (100 bytes) are externalised by the output layer.
func taintRegistry() *tools.Registry {
	reg := tools.NewRegistry()
	result := func(content string, meta map[string]any) func() (*agentcore.ToolResult, error) {
		return func() (*agentcore.ToolResult, error) {
			return &agentcore.ToolResult{Content: content, Metadata: meta}, nil
		}
	}
	big := strings.Repeat("dump line\n", 50)
	reg.Register(metaTool{fnTool{name: "web_like", fn: result("page", nil)}, tools.Meta{Category: tools.CategoryFetch, ReadOnly: true, External: true}})
	reg.Register(metaTool{fnTool{name: "web_like_err", fn: func() (*agentcore.ToolResult, error) {
		return &agentcore.ToolResult{Content: "403 from host", IsError: true}, nil
	}}, tools.Meta{External: true}})
	reg.Register(fnTool{name: "mem_get", fn: result("note", map[string]any{"external": true})})
	reg.Register(fnTool{name: "shell_exec", fn: result("ok", nil)})
	reg.Register(fnTool{name: "shell_big", fn: result(big, nil)})
	reg.Register(fnTool{name: "file_list", fn: result(big, nil)})
	reg.Register(fnTool{name: "plain", fn: result("local", nil)})
	reg.Register(fnTool{name: "broken", fn: func() (*agentcore.ToolResult, error) { return nil, errors.New("boom") }})
	return reg
}

// runTaintTurn runs one turn calling the named tools in order and returns
// the session meta afterwards.
func runTaintTurn(t *testing.T, repo *store.Store, sessionID string, mut func(*Config), calls ...string) (string, map[string]any) {
	t.Helper()
	steps := make([]mock.Step, 0, len(calls)+1)
	for i, name := range calls {
		steps = append(steps, mock.ToolCall("c"+string(rune('a'+i)), name, map[string]any{}))
	}
	steps = append(steps, mock.Final("done"))
	cfg := Config{Repository: repo, Provider: mock.NewProvider(steps...), Tools: taintRegistry(), Model: "m-1", ProviderName: "p-1",
		InlineOutputBytes: 100, Spiller: NewSessionSpiller(repoTmp(t))}
	if mut != nil {
		mut(&cfg)
	}
	runner, err := New(cfg)
	assert.Require(t, assert.NoErr(t, err))
	res, err := runner.Run(context.Background(), RunRequest{SessionID: sessionID, Prompt: "go"})
	assert.Require(t, assert.NoErr(t, err))
	sess, err := repo.GetSession(context.Background(), res.SessionID)
	assert.Require(t, assert.NoErr(t, err))
	return res.SessionID, sess.Meta
}

func TestSessionTaint(t *testing.T) {
	defaultSpill := func(c *Config) { c.TaintOnSpill = []string{"shell_exec", "shell_big"} }
	for _, tc := range []struct {
		name  string
		mut   func(*Config)
		calls []string
		by    string // "" = not tainted
	}{
		{"External tool", nil, []string{"plain", "web_like"}, "web_like"},
		{"External tool error result", nil, []string{"web_like_err"}, "web_like_err"},
		{"result metadata external", nil, []string{"mem_get"}, "mem_get"},
		{"local tools only", nil, []string{"plain", "shell_exec", "broken"}, ""},
		{"configured taint tool", func(c *Config) { c.TaintTools = []string{"shell_exec"} }, []string{"shell_exec"}, "shell_exec"},
		{"shell small output with taint_on_spill", defaultSpill, []string{"shell_exec"}, ""},
		{"shell spilled output with taint_on_spill", defaultSpill, []string{"shell_big"}, "shell_big"},
		{"taint_on_spill empty: spilled output does not taint", func(c *Config) { c.TaintOnSpill = []string{} }, []string{"shell_big"}, ""},
		{"spilled tool not in taint_on_spill", defaultSpill, []string{"file_list"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, meta := runTaintTurn(t, openObserveRepo(t), "", tc.mut, tc.calls...)
			if tc.by == "" {
				assert.Nil(t, meta[metaTaintedAt])
				assert.Nil(t, meta[metaTaintedBy])
				return
			}
			assert.Eq(t, tc.by, meta[metaTaintedBy])
			at, _ := meta[metaTaintedAt].(string)
			_, err := time.Parse(time.RFC3339, at)
			assert.NoErr(t, err, at)
		})
	}

	t.Run("written once, kept across turns and runners", func(t *testing.T) {
		repo := openObserveRepo(t)
		sid, meta := runTaintTurn(t, repo, "", nil, "web_like", "mem_get")
		assert.Eq(t, "web_like", meta[metaTaintedBy])
		first := meta[metaTaintedAt]
		// A new runner (no cached flag) and a later External tool.
		_, meta = runTaintTurn(t, repo, sid, nil, "mem_get", "plain")
		assert.Eq(t, "web_like", meta[metaTaintedBy])
		assert.Eq(t, first, meta[metaTaintedAt])

		// turn_context reports the flag as of each turn's start.
		ctxEvents := eventsOf(t, repo, sid, store.EventTurnContext)
		assert.Require(t, assert.Eq(t, 2, len(ctxEvents)))
		assert.Eq(t, false, ctxEvents[0].Attrs["tainted"])
		assert.Eq(t, true, ctxEvents[1].Attrs["tainted"])
	})
}

// failingMeta fails every write.
type failingMeta struct{ calls int }

func (f *failingMeta) PatchSessionMetaIfAbsent(context.Context, string, string, map[string]any) (bool, error) {
	f.calls++
	return false, errors.New("database is locked")
}

func TestTaintMarkerWriteFailureIsOnlyLogged(t *testing.T) {
	meta := &failingMeta{}
	m := newTaintMarker(meta, []string{"x"}, nil)
	ctx := withObserveScope(context.Background(), "s1", "t1")
	res := &agentcore.ToolResult{Content: "c"}
	m.observe(ctx, nil, "x", res)
	m.observe(ctx, nil, "x", res) // not cached as tainted: retried
	assert.Eq(t, 2, meta.calls)
	m.observe(context.Background(), nil, "x", res) // no session: nothing to flag
	m.observe(ctx, nil, "other", res)              // not a taint source
	assert.Eq(t, 2, meta.calls)

	t.Run("a failing store does not fail the turn", func(t *testing.T) {
		repo := openObserveRepo(t)
		runner, err := New(Config{Repository: repo, Tools: taintRegistry(), Provider: mock.NewProvider(
			mock.ToolCall("c1", "web_like", map[string]any{}), mock.Final("done"))})
		assert.NoErr(t, err)
		runner.rec.taint.meta = meta
		res, err := runner.Run(context.Background(), RunRequest{Prompt: "go"})
		assert.NoErr(t, err)
		assert.Eq(t, RunCompleted, res.State)
		assert.Eq(t, 3, meta.calls)
	})
}

func TestTurnContextEvent(t *testing.T) {
	repo := openObserveRepo(t)
	reg := taintRegistry()
	runner, err := New(Config{Repository: repo, Tools: reg, Provider: mock.NewProvider(mock.Final("a")),
		Model: "m-1", ProviderName: "p-1", Mode: ModeReadOnly,
		RuleSource: func() (string, string, error) { return "be brief", "rules-hash-123456", nil }})
	assert.NoErr(t, err)
	res, err := runner.Run(context.Background(), RunRequest{Prompt: "hi"})
	assert.NoErr(t, err)
	events := eventsOf(t, repo, res.SessionID, store.EventTurnContext)
	assert.Require(t, assert.Eq(t, 1, len(events)))
	e := events[0]
	assert.Eq(t, res.TurnID, e.TurnID)
	assert.Eq(t, store.EventTurnContext, e.Name)
	names := reg.List()
	assert.Eq(t, map[string]any{
		"model": "m-1", "provider": "p-1", "mode": ModeReadOnly,
		"tools": toolSetHash(names), "tools_count": float64(len(names)),
		"rules_hash": "rules-hash-123456", "memory_hash": "", "tainted": false,
		// The new session's start record holds the segment snapshot.
		"segment_hash": segmentRecords(t, repo, res.SessionID)[0].Context.Hash,
	}, e.Attrs)

	t.Run("tool set hash ignores order", func(t *testing.T) {
		assert.Eq(t, toolSetHash([]string{"a", "b"}), toolSetHash([]string{"b", "a"}))
		assert.NotEq(t, toolSetHash([]string{"a", "b"}), toolSetHash([]string{"a", "c"}))
		assert.Eq(t, toolSetHashLen, len(toolSetHash(nil)))
	})
	// The hashes come from the adopted segment record (plus context updates),
	// never from meta: meta is only a cache that may run ahead of the records.
	t.Run("memory hash comes from the segment record, mode from config", func(t *testing.T) {
		assert.NoErr(t, repo.PatchSessionMeta(context.Background(), res.SessionID, map[string]any{metaMemoryIndexHash: "memhash9876"}))
		runner, err := New(Config{Repository: repo, Provider: mock.NewProvider(mock.Final("b"))})
		assert.NoErr(t, err)
		_, err = runner.Run(context.Background(), RunRequest{SessionID: res.SessionID, Prompt: "again"})
		assert.NoErr(t, err)
		events := eventsOf(t, repo, res.SessionID, store.EventTurnContext)
		assert.Require(t, assert.Eq(t, 2, len(events)))
		assert.Eq(t, "", events[1].Attrs["memory_hash"])
		assert.Eq(t, "rules-hash-123456", events[1].Attrs["rules_hash"])
		assert.Eq(t, "unknown", events[1].Attrs["mode"])
		assert.Eq(t, float64(0), events[1].Attrs["tools_count"])
	})
}

func TestTraceShowsTurnContext(t *testing.T) {
	at := time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC)
	e := store.Event{Kind: store.EventTurnContext, Name: store.EventTurnContext, Status: eventOK, TurnID: "turn_1", StartedAt: at,
		Attrs: map[string]any{"model": "m-1", "provider": "p-1", "mode": ModeAllowWrite, "tools": "abcdef123456",
			"tools_count": float64(9), "rules_hash": "0123456789abcdef", "memory_hash": "", "tainted": true}}
	var text, js strings.Builder
	assert.NoErr(t, WriteTrace(&text, []store.Event{e}, nil, false))
	assert.Eq(t, at.Local().Format("2006-01-02T15:04:05.000")+
		" turn_context model=p-1/m-1 mode=allow_write tools=9#abcdef12 tainted=true rules=01234567 memory=- turn=turn_1\n", text.String())
	assert.NoErr(t, WriteTrace(&js, []store.Event{e}, nil, true))
	assert.StrContains(t, js.String(), `"kind":"turn_context"`)
	assert.StrContains(t, js.String(), `"memory_hash":""`)
	assert.StrContains(t, js.String(), `"tools":"abcdef123456"`)
}
