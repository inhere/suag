package memory

import (
	"strings"

	"github.com/inhere/axon/pkg/agentcore/api"
)

// SummaryInjector controls how a summary string becomes model-facing messages.
type SummaryInjector interface {
	InjectSummary(summary string, state *api.RunState) []api.Message
}

// DefaultSummaryInjector emits one system message with a stable summary prefix.
type DefaultSummaryInjector struct{}

// InjectSummary converts a summary string into a single system message.
func (DefaultSummaryInjector) InjectSummary(summary string, _ *api.RunState) []api.Message {
	if strings.TrimSpace(summary) == "" {
		return nil
	}
	return []api.Message{{
		Role:    "system",
		Content: "[Memory summary]\n" + summary,
	}}
}
