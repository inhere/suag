package api

import "context"

// ContextBuilder assembles model-facing messages from the current public run state.
type ContextBuilder interface {
	BuildMessages(ctx context.Context, state *RunState) ([]Message, error)
}
