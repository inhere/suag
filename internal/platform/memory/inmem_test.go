package memory

import (
	"context"
	"strings"
	"testing"
)

func TestInMemStoreMemory_AddAndGetMessages(t *testing.T) {
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

func TestInMemStoreMemory_GetContext_NilSummarizer(t *testing.T) {
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

func TestInMemStoreMemory_SearchSimilar_Empty(t *testing.T) {
	s := NewInMemStore(InMemConfig{})
	results, err := s.SearchSimilar(context.Background(), "anything", 5)
	if err != nil {
		t.Fatalf("SearchSimilar returned error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results, got %v", results)
	}
}

func TestInMemStoreMemory_GetContext_PrependsSummaryAndKeepsRecentWindow(t *testing.T) {
	s := NewInMemStore(InMemConfig{MaxMessages: 3, KeepRecent: 2})

	s.AddMessage("user", "one")
	s.AddMessage("assistant", "two")
	s.AddMessage("user", "three")
	s.AddMessage("assistant", "four")

	msgs, err := s.GetContext(context.Background(), 8000)
	if err != nil {
		t.Fatalf("GetContext error: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("context messages = %d, want 3", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Fatalf("first context role = %q, want system summary", msgs[0].Role)
	}
	if !strings.Contains(msgs[0].Content, "user: one") || !strings.Contains(msgs[0].Content, "assistant: two") {
		t.Fatalf("summary content = %q, want compacted older messages", msgs[0].Content)
	}
	if msgs[1].Content != "three" || msgs[2].Content != "four" {
		t.Fatalf("recent window = %#v, want [three four]", msgs[1:])
	}
}
