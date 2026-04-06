package memory

import (
	"strings"

	"github.com/inhere/axon/pkg/agentcore"
)

// SummaryInjector controls how a summary string becomes model-facing messages.
type SummaryInjector interface {
	InjectSummary(summary string, state *agentcore.RunState) []agentcore.Message
}

// DefaultSummaryInjector emits one system message with a stable summary prefix.
type DefaultSummaryInjector struct{}

// InjectSummary converts a summary string into a single system message.
func (DefaultSummaryInjector) InjectSummary(summary string, _ *agentcore.RunState) []agentcore.Message {
	if strings.TrimSpace(summary) == "" {
		return nil
	}
	return []agentcore.Message{{
		Role:    "system",
		Content: "[Memory summary]\n" + summary,
	}}
}
