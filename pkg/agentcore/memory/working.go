package memory

import (
	"context"

	"github.com/inhere/axon/pkg/agentcore"
	internalutils "github.com/inhere/axon/pkg/agentcore/internal/utils"
)

// Config controls how WorkingMemory filters and emits model-facing context.
type Config struct {
	MaxRecentMessages     int
	IncludeSystemMessages bool
	IncludeToolMessages   bool
	SummaryInjector       SummaryInjector
}

// WorkingMemory is a small first-party implementation of agentcore.Memory.
//
// It keeps an optional summary string plus a filtered recent-message window.
type WorkingMemory struct {
	config  Config
	summary string
	recent  []agentcore.Message
}

// New creates a WorkingMemory with the provided config.
func New(cfg Config) *WorkingMemory {
	if cfg.SummaryInjector == nil {
		cfg.SummaryInjector = DefaultSummaryInjector{}
	}
	return &WorkingMemory{config: cfg}
}

// SetSummary updates the current summary slot used during BuildContext.
func (m *WorkingMemory) SetSummary(summary string) {
	if m == nil {
		return
	}
	m.summary = summary
}

// Summary returns the current summary slot value.
func (m *WorkingMemory) Summary() string {
	if m == nil {
		return ""
	}
	return m.summary
}

// Hydrate rebuilds the recent-message window from the authoritative transcript.
func (m *WorkingMemory) Hydrate(_ context.Context, state *agentcore.RunState) error {
	if m == nil {
		return nil
	}
	if state == nil {
		m.recent = nil
		return nil
	}
	m.recent = m.filterAndTruncate(state.Messages)
	return nil
}

// ObserveMessages incrementally updates the recent-message window.
func (m *WorkingMemory) ObserveMessages(_ context.Context, _ *agentcore.RunState, messages ...agentcore.Message) error {
	if m == nil || len(messages) == 0 {
		return nil
	}
	merged := append(cloneMessages(m.recent), cloneMessages(messages)...)
	m.recent = m.filterAndTruncate(merged)
	return nil
}

// BuildContext emits summary messages first, then the recent-message window.
func (m *WorkingMemory) BuildContext(_ context.Context, state *agentcore.RunState) ([]agentcore.Message, error) {
	if m == nil {
		return nil, nil
	}
	out := m.config.SummaryInjector.InjectSummary(m.summary, state)
	out = append(out, cloneMessages(m.recent)...)
	return out, nil
}

func (m *WorkingMemory) filterAndTruncate(messages []agentcore.Message) []agentcore.Message {
	if len(messages) == 0 {
		return nil
	}

	filtered := make([]agentcore.Message, 0, len(messages))
	for _, msg := range messages {
		if !m.config.IncludeSystemMessages && msg.Role == "system" {
			continue
		}
		if !m.config.IncludeToolMessages && isToolMessage(msg) {
			continue
		}
		filtered = append(filtered, cloneMessage(msg))
	}

	if m.config.MaxRecentMessages > 0 && len(filtered) > m.config.MaxRecentMessages {
		filtered = filtered[len(filtered)-m.config.MaxRecentMessages:]
	}
	return filtered
}

func isToolMessage(msg agentcore.Message) bool {
	return msg.Role == "tool" || msg.ToolCall != nil || msg.ToolResult != nil
}

func cloneMessages(messages []agentcore.Message) []agentcore.Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]agentcore.Message, len(messages))
	for i, msg := range messages {
		cloned[i] = cloneMessage(msg)
	}
	return cloned
}

func cloneMessage(msg agentcore.Message) agentcore.Message {
	cloned := agentcore.Message{
		Role:    msg.Role,
		Content: msg.Content,
	}
	if msg.Metadata != nil {
		cloned.Metadata = cloneMetadata(msg.Metadata)
	}
	if msg.ToolCall != nil {
		cloned.ToolCall = &agentcore.ToolCall{
			CallID:    msg.ToolCall.CallID,
			Name:      msg.ToolCall.Name,
			Arguments: cloneMetadata(msg.ToolCall.Arguments),
		}
	}
	if msg.ToolResult != nil {
		cloned.ToolResult = &agentcore.ToolResult{
			CallID:    msg.ToolResult.CallID,
			Name:      msg.ToolResult.Name,
			Content:   msg.ToolResult.Content,
			Artifacts: cloneArtifacts(msg.ToolResult.Artifacts),
			Metadata:  cloneMetadata(msg.ToolResult.Metadata),
		}
	}
	return cloned
}

func cloneArtifacts(artifacts []agentcore.Artifact) []agentcore.Artifact {
	if len(artifacts) == 0 {
		return nil
	}
	cloned := make([]agentcore.Artifact, len(artifacts))
	for i, artifact := range artifacts {
		cloned[i] = agentcore.Artifact{
			Kind:     artifact.Kind,
			URI:      artifact.URI,
			Content:  artifact.Content,
			Metadata: cloneMetadata(artifact.Metadata),
		}
	}
	return cloned
}

func cloneMetadata(src map[string]any) map[string]any {
	return internalutils.CloneMetadata(src)
}
