package internalmemory

import (
	"context"
	"errors"
	"testing"

	"github.com/inhere/axon/internal/platform/memory"
	"github.com/inhere/axon/pkg/agentcore/api"
)

type testStore struct {
	messages []memory.Message
}

func (s *testStore) AddMessage(role, content string) {
	s.messages = append(s.messages, memory.Message{Role: role, Content: content})
}

func (s *testStore) GetMessages() []memory.Message {
	out := make([]memory.Message, len(s.messages))
	copy(out, s.messages)
	return out
}

func (s *testStore) GetContext(context.Context, int) ([]memory.Message, error) {
	return s.GetMessages(), nil
}

func (s *testStore) SaveFragment(context.Context, string, string) error { return nil }

func (s *testStore) SearchSimilar(context.Context, string, int) ([]memory.Fragment, error) {
	return nil, nil
}

func (s *testStore) Close() error { return nil }

func TestMemoryAdapter_Hydrate_ReturnsConflictOnDivergedPrefix(t *testing.T) {
	store := &testStore{
		messages: []memory.Message{
			{Role: "user", Content: "local"},
		},
	}
	adapter := New(store)

	err := adapter.Hydrate(context.Background(), &api.RunState{
		RunID: "run-1",
		Messages: []api.Message{
			{Role: "user", Content: "authoritative"},
		},
	})
	if !errors.Is(err, api.ErrMemoryStateConflict) {
		t.Fatalf("expected memory state conflict, got %v", err)
	}
}

func TestMemoryAdapter_Hydrate_AppendsMissingTranscriptTail(t *testing.T) {
	store := &testStore{
		messages: []memory.Message{
			{Role: "user", Content: "hello"},
		},
	}
	adapter := New(store)

	err := adapter.Hydrate(context.Background(), &api.RunState{
		RunID: "run-1",
		Messages: []api.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "world"},
		},
	})
	if err != nil {
		t.Fatalf("expected hydrate to append missing tail, got %v", err)
	}
	got := store.GetMessages()
	if len(got) != 2 || got[1].Role != "assistant" || got[1].Content != "world" {
		t.Fatalf("messages = %#v, want appended transcript tail", got)
	}
}

func TestMemoryAdapter_ObserveMessages_CompactsToolResultsAndSkipsEmpty(t *testing.T) {
	store := &testStore{}
	adapter := New(store)

	err := adapter.ObserveMessages(context.Background(), &api.RunState{RunID: "run-1"},
		api.Message{Role: "assistant", Content: "done"},
		api.Message{
			Role: "tool",
			ToolResult: &api.ToolResult{
				Name:    "search",
				Content: "hit-1",
			},
		},
		api.Message{
			Role: "tool",
			ToolResult: &api.ToolResult{
				Name:    "bash",
				Content: "tool error: permission denied",
				Metadata: map[string]any{
					"tool_error": true,
				},
			},
		},
		api.Message{Role: "system", Content: ""},
	)
	if err != nil {
		t.Fatalf("ObserveMessages error: %v", err)
	}

	got := store.GetMessages()
	if len(got) != 3 {
		t.Fatalf("messages = %#v, want 3 compacted messages", got)
	}
	if got[0].Role != "assistant" || got[0].Content != "done" {
		t.Fatalf("first message = %#v, want assistant content", got[0])
	}
	if got[1].Role != "tool" || got[1].Content != "hit-1" {
		t.Fatalf("second message = %#v, want tool result content", got[1])
	}
	if got[2].Role != "tool" || got[2].Content != `tool "bash" error: permission denied` {
		t.Fatalf("third message = %#v, want normalized tool error", got[2])
	}
}

func TestMemoryAdapter_BuildContext_MapsStoreMessages(t *testing.T) {
	store := &testStore{
		messages: []memory.Message{
			{Role: "system", Content: "[Memory summary]: earlier"},
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "world"},
		},
	}
	adapter := New(store)

	got, err := adapter.BuildContext(context.Background(), &api.RunState{RunID: "run-1"})
	if err != nil {
		t.Fatalf("BuildContext error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("context messages = %#v, want 3", got)
	}
	if got[0].Role != "system" || got[0].Content != "[Memory summary]: earlier" {
		t.Fatalf("first context message = %#v, want summary", got[0])
	}
	if got[2].Role != "assistant" || got[2].Content != "world" {
		t.Fatalf("last context message = %#v, want assistant world", got[2])
	}
}
