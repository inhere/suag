package prompt

import (
	"context"

	"github.com/inhere/axon/pkg/agentcore/api"
)

// StaticBuilder always returns the same system prompt.
type StaticBuilder struct {
	Prompt string
}

// BuildSystemPrompt satisfies the agentcore prompt builder contract.
func (b StaticBuilder) BuildSystemPrompt(context.Context, *api.RunState) (string, error) {
	return b.Prompt, nil
}
