package internalmemory

import (
	"context"
	"errors"
	"testing"

	"github.com/inhere/axon/internal/memory"
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

func TestAdapter_Hydrate_ReturnsConflictOnDivergedPrefix(t *testing.T) {
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

func TestAdapter_Hydrate_AppendsMissingTranscriptTail(t *testing.T) {
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
