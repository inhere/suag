package context_test

import (
	stdctx "context"
	"errors"
	"fmt"
	"testing"

	"github.com/inhere/axon/pkg/agentcore"
	builderctx "github.com/inhere/axon/pkg/agentcore/context"
)

type contextTestMemory struct {
	context  []agentcore.Message
	buildErr error
}

func (m *contextTestMemory) Hydrate(stdctx.Context, *agentcore.RunState) error { return nil }

func (m *contextTestMemory) ObserveMessages(stdctx.Context, *agentcore.RunState, ...agentcore.Message) error {
	return nil
}

func (m *contextTestMemory) BuildContext(stdctx.Context, *agentcore.RunState) ([]agentcore.Message, error) {
	if m.buildErr != nil {
		return nil, m.buildErr
	}
	if len(m.context) == 0 {
		return nil, nil
	}
	msgs := make([]agentcore.Message, len(m.context))
	copy(msgs, m.context)
	return msgs, nil
}

func TestDefaultContextBuilder_Truncates(t *testing.T) {
	builder := builderctx.DefaultBuilder{MaxMessages: 3}

	msgs, err := builder.BuildMessages(stdctx.Background(), stateWithMessages(5))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "message-3" || msgs[2].Content != "message-5" {
		t.Fatalf("expected the latest messages, got %#v", msgs)
	}
}

func TestTranscriptWindow_TruncatesLatestMessages(t *testing.T) {
	builder := builderctx.TranscriptWindow(builderctx.WindowConfig{MaxMessages: 2})

	msgs, err := builder.BuildMessages(stdctx.Background(), stateWithMessages(4))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %#v", msgs)
	}
	if msgs[0].Content != "message-3" || msgs[1].Content != "message-4" {
		t.Fatalf("expected the last 2 messages, got %#v", msgs)
	}
}

func TestTranscriptWindow_ReturnsCopies(t *testing.T) {
	state := stateWithMessages(2)
	builder := builderctx.TranscriptWindow(builderctx.WindowConfig{})

	msgs, err := builder.BuildMessages(stdctx.Background(), state)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	msgs[0].Content = "mutated"

	if state.Messages[0].Content != "message-1" {
		t.Fatalf("expected original state to remain unchanged, got %#v", state.Messages)
	}
}

func TestMemoryFirst_UsesMemoryWhenPresent(t *testing.T) {
	mem := &contextTestMemory{
		context: []agentcore.Message{{Role: "system", Content: "from-memory"}},
	}
	builder := builderctx.MemoryFirst(mem, builderctx.MemoryFirstConfig{MaxMessages: 2})

	msgs, err := builder.BuildMessages(stdctx.Background(), stateWithMessages(4))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "from-memory" {
		t.Fatalf("expected memory output to win, got %#v", msgs)
	}
}

func TestMemoryFirst_FallsBackOnlyWhenMemoryNil(t *testing.T) {
	builder := builderctx.MemoryFirst(nil, builderctx.MemoryFirstConfig{MaxMessages: 2})

	msgs, err := builder.BuildMessages(stdctx.Background(), stateWithMessages(4))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected fallback transcript window, got %#v", msgs)
	}
	if msgs[0].Content != "message-3" || msgs[1].Content != "message-4" {
		t.Fatalf("expected latest transcript messages, got %#v", msgs)
	}
}

func TestMemoryFirst_DoesNotFallbackOnEmptyMemoryContext(t *testing.T) {
	mem := &contextTestMemory{}
	builder := builderctx.MemoryFirst(mem, builderctx.MemoryFirstConfig{MaxMessages: 2})

	msgs, err := builder.BuildMessages(stdctx.Background(), stateWithMessages(4))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if msgs != nil {
		t.Fatalf("expected empty memory result to be preserved, got %#v", msgs)
	}
}

func TestMemoryFirst_PropagatesMemoryErrors(t *testing.T) {
	mem := &contextTestMemory{buildErr: errors.New("memory failed")}
	builder := builderctx.MemoryFirst(mem, builderctx.MemoryFirstConfig{})

	_, err := builder.BuildMessages(stdctx.Background(), stateWithMessages(1))
	if err == nil || err.Error() != "memory failed" {
		t.Fatalf("expected memory error to be returned, got %v", err)
	}
}

func TestMemoryThenRecent_AppendsRecentTranscriptAfterMemory(t *testing.T) {
	mem := &contextTestMemory{
		context: []agentcore.Message{{Role: "system", Content: "from-memory"}},
	}
	state := &agentcore.RunState{
		Messages: []agentcore.Message{
			{Role: "user", Content: "message-1"},
			{Role: "assistant", Content: "message-2"},
			{Role: "user", Content: "message-3"},
			{Role: "assistant", Content: "message-4"},
		},
	}
	builder := builderctx.MemoryThenRecent(mem, builderctx.MemoryThenRecentConfig{MaxMessages: 2})

	msgs, err := builder.BuildMessages(stdctx.Background(), state)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected memory + 2 recent transcript messages, got %#v", msgs)
	}
	if msgs[0].Content != "from-memory" {
		t.Fatalf("expected memory message first, got %#v", msgs)
	}
	if msgs[1].Content != "message-3" || msgs[2].Content != "message-4" {
		t.Fatalf("expected recent transcript window after memory output, got %#v", msgs)
	}
}

func TestMemoryThenRecent_UsesTranscriptWindowWhenMemoryNil(t *testing.T) {
	builder := builderctx.MemoryThenRecent(nil, builderctx.MemoryThenRecentConfig{MaxMessages: 2})

	msgs, err := builder.BuildMessages(stdctx.Background(), stateWithMessages(4))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected transcript fallback, got %#v", msgs)
	}
	if msgs[0].Content != "message-3" || msgs[1].Content != "message-4" {
		t.Fatalf("expected transcript fallback to preserve latest messages, got %#v", msgs)
	}
}

func TestMemoryThenRecent_DoesNotDeduplicateMessages(t *testing.T) {
	mem := &contextTestMemory{
		context: []agentcore.Message{{Role: "user", Content: "message-4"}},
	}
	state := &agentcore.RunState{
		Messages: []agentcore.Message{
			{Role: "user", Content: "message-1"},
			{Role: "assistant", Content: "message-2"},
			{Role: "user", Content: "message-3"},
			{Role: "user", Content: "message-4"},
		},
	}
	builder := builderctx.MemoryThenRecent(mem, builderctx.MemoryThenRecentConfig{MaxMessages: 2})

	msgs, err := builder.BuildMessages(stdctx.Background(), state)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected duplicate content to be preserved, got %#v", msgs)
	}
	if msgs[0].Content != "message-4" || msgs[2].Content != "message-4" {
		t.Fatalf("expected duplicate content from memory and transcript to remain, got %#v", msgs)
	}
}

func stateWithMessages(count int) *agentcore.RunState {
	state := &agentcore.RunState{
		RunID:    "run-1",
		AgentID:  "agent-1",
		Messages: make([]agentcore.Message, 0, count),
	}
	for i := 1; i <= count; i++ {
		state.Messages = append(state.Messages, agentcore.Message{
			Role:    "user",
			Content: fmt.Sprintf("message-%d", i),
		})
	}
	return state
}
