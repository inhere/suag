package context

import (
	"context"
	"strings"

	"github.com/inhere/suag/pkg/agentcore/api"
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

// ArtifactContextConfig controls how artifacts are projected into model context.
type ArtifactContextConfig struct {
	MaxMessages int
	Filter      func(api.Artifact) bool
	Formatter   func(api.Artifact) api.Message
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

// ArtifactsThenRecent returns a first-party preset that appends projected
// artifact messages after a recent transcript window.
func ArtifactsThenRecent(cfg ArtifactContextConfig) api.ContextBuilder {
	return artifactsThenRecentBuilder{
		maxMessages: cfg.MaxMessages,
		filter:      cfg.Filter,
		formatter:   cfg.Formatter,
	}
}

// DefaultArtifactFormatter projects an artifact into a system message and
// includes inline content when available.
func DefaultArtifactFormatter(artifact api.Artifact) api.Message {
	content := "Artifact(" + artifact.Kind + "): " + artifact.URI
	if artifact.Content != "" {
		content += "\n" + artifact.Content
	}
	return api.Message{
		Role:    "system",
		Content: content,
	}
}

// CompactArtifactFormatter projects only artifact identity without inline content.
func CompactArtifactFormatter(artifact api.Artifact) api.Message {
	return api.Message{
		Role:    "system",
		Content: "Artifact(" + artifact.Kind + "): " + artifact.URI,
	}
}

// ArtifactKindFilter returns a filter that matches only the provided kinds.
func ArtifactKindFilter(kinds ...string) func(api.Artifact) bool {
	allowed := make(map[string]struct{}, len(kinds))
	for _, kind := range kinds {
		trimmed := strings.TrimSpace(kind)
		if trimmed == "" {
			continue
		}
		allowed[trimmed] = struct{}{}
	}
	return func(artifact api.Artifact) bool {
		if len(allowed) == 0 {
			return true
		}
		_, ok := allowed[artifact.Kind]
		return ok
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

type artifactsThenRecentBuilder struct {
	maxMessages int
	filter      func(api.Artifact) bool
	formatter   func(api.Artifact) api.Message
}

func (b artifactsThenRecentBuilder) BuildMessages(ctx context.Context, state *api.RunState) ([]api.Message, error) {
	recent, err := transcriptWindowBuilder{maxMessages: b.maxMessages}.BuildMessages(ctx, state)
	if err != nil {
		return nil, err
	}
	projected := b.projectArtifacts(state)
	if len(projected) == 0 {
		return recent, nil
	}
	out := make([]api.Message, 0, len(recent)+len(projected))
	out = append(out, recent...)
	out = append(out, projected...)
	return out, nil
}

func (b artifactsThenRecentBuilder) projectArtifacts(state *api.RunState) []api.Message {
	if state == nil || len(state.Artifacts) == 0 {
		return nil
	}
	out := make([]api.Message, 0, len(state.Artifacts))
	for _, artifact := range state.Artifacts {
		if b.filter != nil && !b.filter(cloneArtifact(artifact)) {
			continue
		}
		msg := b.formatArtifact(artifact)
		out = append(out, cloneMessage(msg))
	}
	return out
}

func (b artifactsThenRecentBuilder) formatArtifact(artifact api.Artifact) api.Message {
	if b.formatter != nil {
		return b.formatter(cloneArtifact(artifact))
	}
	return DefaultArtifactFormatter(artifact)
}

func cloneArtifact(artifact api.Artifact) api.Artifact {
	return api.Artifact{
		Kind:     artifact.Kind,
		URI:      artifact.URI,
		Content:  artifact.Content,
		Metadata: cloneMetadata(artifact.Metadata),
	}
}

func cloneMessage(msg api.Message) api.Message {
	return api.Message{
		Role:       msg.Role,
		Content:    msg.Content,
		ToolCall:   cloneToolCall(msg.ToolCall),
		ToolResult: cloneToolResult(msg.ToolResult),
		Metadata:   cloneMetadata(msg.Metadata),
		Reasoning:  msg.Reasoning,
	}
}

func cloneToolCall(call *api.ToolCall) *api.ToolCall {
	if call == nil {
		return nil
	}
	cloned := *call
	cloned.Arguments = cloneMetadata(call.Arguments)
	return &cloned
}

func cloneToolResult(result *api.ToolResult) *api.ToolResult {
	if result == nil {
		return nil
	}
	cloned := *result
	if len(result.Artifacts) > 0 {
		cloned.Artifacts = make([]api.Artifact, len(result.Artifacts))
		for i, artifact := range result.Artifacts {
			cloned.Artifacts[i] = cloneArtifact(artifact)
		}
	}
	cloned.Metadata = cloneMetadata(result.Metadata)
	return &cloned
}

func cloneMetadata(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
