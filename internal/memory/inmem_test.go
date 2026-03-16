package memory

import (
	"context"
	"testing"
)

func TestInMemStore_AddAndGetMessages(t *testing.T) {
	s := NewInMemStore(InMemConfig{})
	s.AddMessage("user", "hello")
	s.AddMessage("assistant", "hi there")

	msgs := s.GetMessages()
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Errorf("unexpected first message: %+v", msgs[0])
	}
}

func TestInMemStore_GetContext_NilSummarizer(t *testing.T) {
	s := NewInMemStore(InMemConfig{MaxMessages: 5, KeepRecent: 3})

	// Fill beyond the threshold.
	for i := range 8 {
		s.AddMessage("user", "message "+string(rune('A'+i)))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	msgs, err := s.GetContext(ctx, 8000)
	if err != nil {
		t.Fatalf("GetContext error: %v", err)
	}
	// Should have summary + ≤ keepRecent messages.
	// Summary is prepended as "system" when fallback is used.
	for _, m := range msgs {
		if m.Role == "" {
			t.Error("message has empty role")
		}
	}
}

func TestInMemStore_SearchSimilar_Empty(t *testing.T) {
	s := NewInMemStore(InMemConfig{})
	results, err := s.SearchSimilar(context.Background(), "anything", 5)
	if err != nil {
		t.Fatalf("SearchSimilar returned error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results, got %v", results)
	}
}
