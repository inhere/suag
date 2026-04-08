package api

import "context"

// PromptBuilder builds the provider system prompt from the current public run state.
type PromptBuilder interface {
	BuildSystemPrompt(ctx context.Context, state *RunState) (string, error)
}
