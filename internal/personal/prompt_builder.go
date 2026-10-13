package personal

import (
	"context"

	"github.com/inhere/suga/pkg/agentcore"
)

// stablePromptBuilder is the system prompt of every turn: only the stable
// prefix (identity and working conventions, design D1). Everything that
// depends on the workspace, mode, rules, memory, skills or deferred tools is
// in the segment context message, so the system prompt never changes inside a
// segment and is byte-identical across sessions and workspaces of the same
// configuration (the provider prefix cache can reuse it).
type stablePromptBuilder struct {
	base agentcore.PromptBuilder
}

func (b stablePromptBuilder) BuildSystemPrompt(ctx context.Context, state *agentcore.RunState) (string, error) {
	if b.base == nil {
		return "", nil
	}
	return b.base.BuildSystemPrompt(ctx, state)
}
