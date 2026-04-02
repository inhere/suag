package context

import (
	"context"

	"github.com/inhere/axon/pkg/agentcore"
)

// DefaultBuilder assembles recent run messages for model context.
type DefaultBuilder struct {
	MaxMessages int
}

// BuildMessages returns a shallow copy of the current run messages, optionally truncated.
func (b DefaultBuilder) BuildMessages(_ context.Context, state *agentcore.RunState) ([]agentcore.Message, error) {
	return b.Messages(state), nil
}

// Messages returns a shallow copy of the most recent run messages.
func (b DefaultBuilder) Messages(state *agentcore.RunState) []agentcore.Message {
	if state == nil || len(state.Messages) == 0 {
		return nil
	}

	start := 0
	if b.MaxMessages > 0 && len(state.Messages) > b.MaxMessages {
		start = len(state.Messages) - b.MaxMessages
	}

	messages := make([]agentcore.Message, len(state.Messages[start:]))
	copy(messages, state.Messages[start:])
	return messages
}
