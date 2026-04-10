package internalmemory

import (
	"context"
	"fmt"
	"strings"

	"github.com/inhere/axon/internal/platform/memory"
	"github.com/inhere/axon/pkg/agentcore/api"
)

// Adapter bridges an internal memory.Store into the public agentcore api.Memory contract.
type Adapter struct {
	store memory.Store
}

// New adapts an internal memory.Store into agentcore api.Memory.
func New(store memory.Store) api.Memory {
	return &Adapter{store: store}
}

func (a *Adapter) Hydrate(_ context.Context, state *api.RunState) error {
	if a == nil || a.store == nil || state == nil {
		return nil
	}
	transcript := compactMessages(state.Messages)
	existing := a.store.GetMessages()
	if len(existing) > len(transcript) {
		return &api.MemoryError{Err: api.ErrMemoryStateConflict}
	}
	for i := range existing {
		if existing[i].Role != transcript[i].Role || existing[i].Content != transcript[i].Content {
			return &api.MemoryError{Err: api.ErrMemoryStateConflict}
		}
	}
	for _, msg := range transcript[len(existing):] {
		a.store.AddMessage(msg.Role, msg.Content)
	}
	return nil
}

func (a *Adapter) ObserveMessages(_ context.Context, _ *api.RunState, messages ...api.Message) error {
	if a == nil || a.store == nil {
		return nil
	}
	for _, msg := range messages {
		content := compactContent(msg)
		if content == "" {
			continue
		}
		a.store.AddMessage(msg.Role, content)
	}
	return nil
}

func (a *Adapter) BuildContext(ctx context.Context, _ *api.RunState) ([]api.Message, error) {
	if a == nil || a.store == nil {
		return nil, nil
	}
	messages, err := a.store.GetContext(ctx, 8000)
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

func compactMessages(messages []api.Message) []memory.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]memory.Message, 0, len(messages))
	for _, msg := range messages {
		content := strings.TrimSpace(compactContent(msg))
		if content == "" {
			continue
		}
		out = append(out, memory.Message{
			Role:    msg.Role,
			Content: content,
		})
	}
	return out
}
