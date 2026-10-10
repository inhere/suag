package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/api"
	"github.com/inhere/suag/pkg/agentcore/tools"
)

type scriptedProvider struct{ results []*agentcore.GenerateResult }

func (p *scriptedProvider) Name() string { return "scripted" }
func (p *scriptedProvider) Generate(_ context.Context, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	r := p.results[0]
	p.results = p.results[1:]
	return r, nil
}

type echoTool struct{}

func (echoTool) Name() string        { return "echo" }
func (echoTool) Description() string { return "echo" }
func (echoTool) Schema() agentcore.ToolSchema {
	return agentcore.ToolSchema{Name: "echo", Description: "echo", InputSchema: map[string]any{"type": "object"}}
}
func (echoTool) Execute(_ context.Context, call agentcore.ToolCall) (*agentcore.ToolResult, error) {
	return &agentcore.ToolResult{CallID: call.CallID, Name: "echo", Content: "echo:" + call.Arguments["text"].(string),
		Metadata: map[string]any{"path": "/x"}}, nil
}

func TestAgentStorageRunRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	newSession(t, s, "s1")
	assert.NoErr(t, s.CreateTurn(ctx, Turn{ID: "turn_1", SessionID: "s1"}))

	reg := tools.NewRegistry()
	reg.Register(echoTool{})
	provider := &scriptedProvider{results: []*agentcore.GenerateResult{
		{Content: "calling first", FinishReason: api.FinishToolCalls, ToolCalls: []agentcore.ToolCall{{CallID: "c1", Name: "echo", Arguments: map[string]any{"text": "a"}}}},
		{Content: "calling second", FinishReason: api.FinishToolCalls, ToolCalls: []agentcore.ToolCall{{CallID: "c2", Name: "echo", Arguments: map[string]any{"text": "b"}}}},
		{Content: "done", FinishReason: api.FinishFinal},
	}}
	rt := agentcore.New(agentcore.Config{Provider: provider, Storage: s.AgentStorage(), Tools: reg})
	res, err := rt.Run(ctx, agentcore.RunInput{RunID: "turn_1", AgentID: "t", Instruction: "go"})
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, "done", res.Output)

	st := s.AgentStorage()
	loaded, err := st.LoadRun(ctx, "turn_1")
	assert.Require(t, assert.NoErr(t, err))
	msgs, err := st.LoadMessages(ctx, "turn_1")
	assert.Require(t, assert.NoErr(t, err))

	// Messages live only in the entries (user + 3 assistant + 2 tool); the
	// state snapshot does not repeat them, and the authority view rebuilds
	// them from the entries.
	assert.Eq(t, 0, len(loaded.Messages))
	turn, err := s.GetTurn(ctx, "turn_1")
	assert.Require(t, assert.NoErr(t, err))
	assert.NotContains(t, string(turn.StateJSON), "calling first")
	assert.Eq(t, 6, len(msgs))
	hydrated, err := agentcore.LoadRunAuthority(ctx, st, "turn_1")
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, msgs, hydrated.Messages)
	t.Run("an old snapshot with messages still loads", func(t *testing.T) {
		old := *hydrated
		raw, err := json.Marshal(&old)
		assert.Require(t, assert.NoErr(t, err))
		assert.NoErr(t, s.UpsertTurnState(ctx, "turn_1", raw))
		again, err := st.LoadRun(ctx, "turn_1")
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, msgs, again.Messages)
	})
	assert.Eq(t, "user", msgs[0].Role)
	assert.Eq(t, "calling first", msgs[1].Content) // assistant text beside a tool call survives
	assert.Eq(t, "c1", msgs[1].ToolCall.CallID)
	assert.Eq(t, "echo:a", msgs[2].ToolResult.Content)
	assert.Eq(t, "/x", msgs[2].ToolResult.Metadata["path"])

	// Entry types and seq are contiguous in the session log.
	entries, _ := s.ListEntries(ctx, "s1", EntryFilter{})
	var types []string
	for i, e := range entries {
		assert.Eq(t, int64(i+1), e.Seq)
		types = append(types, e.Type)
	}
	assert.Eq(t, "user,assistant,tool,assistant,tool,assistant", strings.Join(types, ","))
}

func TestAgentStorageMessageFidelity(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	newSession(t, s, "s1")
	assert.NoErr(t, s.CreateTurn(ctx, Turn{ID: "t1", SessionID: "s1"}))
	st := s.AgentStorage()
	in := []api.Message{
		{Role: "user", Content: "hi 你好 \"q\"", Metadata: map[string]any{"k": "v"}},
		{Role: "assistant", Content: "text", ToolCall: &api.ToolCall{CallID: "c", Name: "n", Arguments: map[string]any{"a": "b", "n": []any{"x"}}}},
		{Role: "tool", ToolResult: &api.ToolResult{CallID: "c", Name: "n", Content: "r", Artifacts: []api.Artifact{{Kind: "k", URI: "u", Content: "c", Metadata: map[string]any{"z": "1"}}}, IsError: true, Metadata: map[string]any{"k": "v"}}},
	}
	assert.NoErr(t, st.AppendMessages(ctx, "t1", in...))
	out, err := st.LoadMessages(ctx, "t1")
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, in, out)
}

func TestAgentStorageMisses(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	newSession(t, s, "s1")
	st := s.AgentStorage()

	t.Run("unknown turn is a storage miss", func(t *testing.T) {
		_, err := st.LoadRun(ctx, "nope")
		assert.Eq(t, true, errors.Is(err, os.ErrNotExist))
		_, err = st.LoadMessages(ctx, "nope")
		assert.Eq(t, true, errors.Is(err, os.ErrNotExist))
		assert.Eq(t, true, errors.Is(st.SaveRun(ctx, &api.RunState{RunID: "nope"}), ErrTurnNotFound))
		assert.Eq(t, true, errors.Is(st.AppendMessages(ctx, "nope", api.Message{Role: "user"}), ErrTurnNotFound))
	})
	t.Run("turn without state is a miss, runtime starts fresh", func(t *testing.T) {
		assert.NoErr(t, s.CreateTurn(ctx, Turn{ID: "t1", SessionID: "s1"}))
		_, err := st.LoadRun(ctx, "t1")
		assert.Eq(t, true, errors.Is(err, os.ErrNotExist))
		msgs, err := st.LoadMessages(ctx, "t1")
		assert.NoErr(t, err)
		assert.Eq(t, 0, len(msgs))

		provider := &scriptedProvider{results: []*agentcore.GenerateResult{{Content: "ok", FinishReason: api.FinishFinal}}}
		rt := agentcore.New(agentcore.Config{Provider: provider, Storage: st})
		res, err := rt.Run(ctx, agentcore.RunInput{RunID: "t1", Instruction: "x"})
		assert.NoErr(t, err)
		assert.Eq(t, "ok", res.Output)
	})
}

func TestAgentStorageTurnsDoNotCrossTalk(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	newSession(t, s, "s1")
	st := s.AgentStorage()
	for _, id := range []string{"t1", "t2"} {
		assert.NoErr(t, s.CreateTurn(ctx, Turn{ID: id, SessionID: "s1"}))
	}
	assert.NoErr(t, st.AppendMessages(ctx, "t1", api.Message{Role: "user", Content: "one"}))
	assert.NoErr(t, st.AppendMessages(ctx, "t2", api.Message{Role: "user", Content: "two"}, api.Message{Role: "assistant", Content: "reply2"}))
	assert.NoErr(t, st.AppendMessages(ctx, "t1", api.Message{Role: "assistant", Content: "reply1"}))
	// A non-message entry in the same session must not leak into LoadMessages.
	_, err := s.AppendEntries(ctx, "s1", NewEntry{TurnID: "t1", Type: EntryContextUpdate, JSON: []byte(`{"x":1}`)})
	assert.NoErr(t, err)

	m1, _ := st.LoadMessages(ctx, "t1")
	m2, _ := st.LoadMessages(ctx, "t2")
	assert.Eq(t, 2, len(m1))
	assert.Eq(t, "one", m1[0].Content)
	assert.Eq(t, "reply1", m1[1].Content)
	assert.Eq(t, 2, len(m2))
	assert.Eq(t, "two", m2[0].Content)
}
