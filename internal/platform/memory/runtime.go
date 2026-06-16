package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/inhere/suag/pkg/agentcore/api"
)

const defaultRuntimeMaxContextTokens = 8000

// RuntimeMemory exposes a memory.Store through the agentcore runtime-facing memory contract.
// It keeps Store as the richer backend and only handles transcript synchronization and context projection.
type RuntimeMemory struct {
	store memoryStoreRuntime
}

type memoryStoreRuntime interface {
	AddMessage(role, content string)
	GetMessages() []Message
	GetContext(ctx context.Context, maxTokens int) ([]Message, error)
}

// NewRuntimeMemory wraps a richer backend store with the runtime-facing api.Memory contract.
func NewRuntimeMemory(store Store) api.Memory {
	if store == nil {
		return &RuntimeMemory{}
	}
	return &RuntimeMemory{store: store}
}

// Hydrate aligns backend memory with the authoritative transcript held by agentcore storage.
func (m *RuntimeMemory) Hydrate(_ context.Context, state *api.RunState) error {
	if m == nil || m.store == nil || state == nil {
		return nil
	}
	transcript := compactMessages(state.Messages)
	existing := m.store.GetMessages()
	if len(existing) > len(transcript) {
		return &api.MemoryError{Err: api.ErrMemoryStateConflict}
	}
	for i := range existing {
		if existing[i].Role != transcript[i].Role || existing[i].Content != transcript[i].Content {
			return &api.MemoryError{Err: api.ErrMemoryStateConflict}
		}
	}
	for _, msg := range transcript[len(existing):] {
		m.store.AddMessage(msg.Role, msg.Content)
	}
	return nil
}

// ObserveMessages projects committed transcript messages into the backend store's working memory view.
func (m *RuntimeMemory) ObserveMessages(_ context.Context, _ *api.RunState, messages ...api.Message) error {
	if m == nil || m.store == nil {
		return nil
	}
	for _, msg := range messages {
		content := compactContent(msg)
		if content == "" {
			continue
		}
		m.store.AddMessage(msg.Role, content)
	}
	return nil
}

// BuildContext reuses the backend store's summarization/window behavior for runtime context assembly.
func (m *RuntimeMemory) BuildContext(ctx context.Context, _ *api.RunState) ([]api.Message, error) {
	if m == nil || m.store == nil {
		return nil, nil
	}
	messages, err := m.store.GetContext(ctx, defaultRuntimeMaxContextTokens)
	if err != nil {
		return nil, err
	}
	out := make([]api.Message, 0, len(messages))
	for _, msg := range messages {
		out = append(out, api.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}
	return out, nil
}

func compactContent(msg api.Message) string {
	if msg.Role == "tool" && msg.ToolResult != nil {
		content := msg.ToolResult.Content
		if flag, _ := msg.ToolResult.Metadata["tool_error"].(bool); flag || strings.HasPrefix(content, "tool error:") {
			name := msg.ToolResult.Name
			if name == "" && msg.ToolCall != nil {
				name = msg.ToolCall.Name
			}
			if name != "" {
				return fmt.Sprintf("tool %q error: %s", name, trimToolErrorPrefix(content))
			}
			return trimToolErrorPrefix(content)
		}
		return content
	}
	return msg.Content
}

func trimToolErrorPrefix(content string) string {
	const prefix = "tool error: "
	if len(content) >= len(prefix) && content[:len(prefix)] == prefix {
		return content[len(prefix):]
	}
	return content
}

func compactMessages(messages []api.Message) []Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]Message, 0, len(messages))
	for _, msg := range messages {
		content := strings.TrimSpace(compactContent(msg))
		if content == "" {
			continue
		}
		out = append(out, Message{
			Role:    msg.Role,
			Content: content,
		})
	}
	return out
}
