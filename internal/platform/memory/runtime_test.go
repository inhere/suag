package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/inhere/axon/pkg/agentcore/api"
)

func TestRuntimeMemory_ImplementsAgentcoreMemory(t *testing.T) {
	var _ api.Memory = NewRuntimeMemory(NewInMemStore(InMemConfig{}))
}

func TestRuntimeMemory_Hydrate_ReturnsConflictOnDivergedPrefix(t *testing.T) {
	store := NewInMemStore(InMemConfig{})
	store.AddMessage("user", "local")

	mem := NewRuntimeMemory(store)

	err := mem.Hydrate(context.Background(), &api.RunState{
		RunID: "run-1",
		Messages: []api.Message{
			{Role: "user", Content: "authoritative"},
		},
	})
	if !errors.Is(err, api.ErrMemoryStateConflict) {
		t.Fatalf("expected memory state conflict, got %v", err)
	}
}

func TestRuntimeMemory_Hydrate_AppendsMissingTranscriptTail(t *testing.T) {
	store := NewInMemStore(InMemConfig{})
	store.AddMessage("user", "hello")

	mem := NewRuntimeMemory(store)

	err := mem.Hydrate(context.Background(), &api.RunState{
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

func TestRuntimeMemory_ObserveMessages_CompactsToolResults(t *testing.T) {
	store := NewInMemStore(InMemConfig{})
	mem := NewRuntimeMemory(store)

	err := mem.ObserveMessages(context.Background(), &api.RunState{RunID: "run-1"},
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

func TestRuntimeMemory_BuildContext_ReusesBackendStoreBehavior(t *testing.T) {
	store := NewInMemStore(InMemConfig{MaxMessages: 3, KeepRecent: 2})
	store.AddMessage("user", "one")
	store.AddMessage("assistant", "two")
	store.AddMessage("user", "three")
	store.AddMessage("assistant", "four")

	mem := NewRuntimeMemory(store)

	got, err := mem.BuildContext(context.Background(), &api.RunState{RunID: "run-1"})
	if err != nil {
		t.Fatalf("BuildContext error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("context messages = %#v, want 3", got)
	}
	if got[0].Role != "system" {
		t.Fatalf("first context message = %#v, want summary", got[0])
	}
	if got[1].Content != "three" || got[2].Content != "four" {
		t.Fatalf("recent context = %#v, want [three four]", got[1:])
	}
}
