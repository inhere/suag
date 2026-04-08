package context

import (
	"context"

	"github.com/inhere/axon/pkg/agentcore/api"
)

// WindowConfig controls transcript-window preset behavior.
type WindowConfig struct {
	MaxMessages int
}

// MemoryFirstConfig controls the fallback transcript window for the memory-first preset.
type MemoryFirstConfig struct {
	MaxMessages int
}

// MemoryThenRecentConfig controls the appended transcript window for the
// memory-then-recent preset.
type MemoryThenRecentConfig struct {
	MaxMessages int
}

// DefaultBuilder assembles recent run messages for model context.
type DefaultBuilder struct {
	MaxMessages int
}

// BuildMessages returns a shallow copy of the current run messages, optionally truncated.
func (b DefaultBuilder) BuildMessages(_ context.Context, state *api.RunState) ([]api.Message, error) {
	return TranscriptWindow(WindowConfig{MaxMessages: b.MaxMessages}).BuildMessages(context.Background(), state)
}

// Messages returns a shallow copy of the most recent run messages.
func (b DefaultBuilder) Messages(state *api.RunState) []api.Message {
	return transcriptWindowBuilder{maxMessages: b.MaxMessages}.messages(state)
}

// TranscriptWindow returns a first-party transcript-only context preset.
func TranscriptWindow(cfg WindowConfig) api.ContextBuilder {
	return transcriptWindowBuilder{maxMessages: cfg.MaxMessages}
}

// MemoryFirst returns a first-party preset that prefers memory output and
// falls back to transcript window only when memory is nil.
func MemoryFirst(mem api.Memory, cfg MemoryFirstConfig) api.ContextBuilder {
	return memoryFirstBuilder{
		mem:         mem,
		maxMessages: cfg.MaxMessages,
	}
}

// MemoryThenRecent returns a first-party preset that uses memory output first
// and then appends a recent transcript window without deduplicating messages.
func MemoryThenRecent(mem api.Memory, cfg MemoryThenRecentConfig) api.ContextBuilder {
	return memoryThenRecentBuilder{
		mem:         mem,
		maxMessages: cfg.MaxMessages,
	}
}

type transcriptWindowBuilder struct {
	maxMessages int
}

func (b transcriptWindowBuilder) BuildMessages(_ context.Context, state *api.RunState) ([]api.Message, error) {
	return b.messages(state), nil
}

func (b transcriptWindowBuilder) messages(state *api.RunState) []api.Message {
	if state == nil || len(state.Messages) == 0 {
		return nil
	}

	start := 0
	if b.maxMessages > 0 && len(state.Messages) > b.maxMessages {
		start = len(state.Messages) - b.maxMessages
	}

	messages := make([]api.Message, len(state.Messages[start:]))
	copy(messages, state.Messages[start:])
	return messages
}

type memoryFirstBuilder struct {
	mem         api.Memory
	maxMessages int
}

func (b memoryFirstBuilder) BuildMessages(ctx context.Context, state *api.RunState) ([]api.Message, error) {
	if b.mem == nil {
		return transcriptWindowBuilder{maxMessages: b.maxMessages}.BuildMessages(ctx, state)
	}
	return b.mem.BuildContext(ctx, state)
}

type memoryThenRecentBuilder struct {
	mem         api.Memory
	maxMessages int
}

func (b memoryThenRecentBuilder) BuildMessages(ctx context.Context, state *api.RunState) ([]api.Message, error) {
	recent, err := transcriptWindowBuilder{maxMessages: b.maxMessages}.BuildMessages(ctx, state)
	if err != nil {
		return nil, err
	}
	if b.mem == nil {
		return recent, nil
	}

	messages, err := b.mem.BuildContext(ctx, state)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return recent, nil
	}
	out := make([]api.Message, 0, len(messages)+len(recent))
	out = append(out, messages...)
	out = append(out, recent...)
	return out, nil
}
