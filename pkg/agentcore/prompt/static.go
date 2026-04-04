package prompt

import (
	"context"

	"github.com/inhere/axon/pkg/agentcore"
)

// StaticBuilder always returns the same system prompt.
type StaticBuilder struct {
	Prompt string
}

// BuildSystemPrompt satisfies the agentcore prompt builder contract.
func (b StaticBuilder) BuildSystemPrompt(context.Context, *agentcore.RunState) (string, error) {
	return b.Prompt, nil
}
