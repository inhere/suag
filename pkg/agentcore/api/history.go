package api

import "context"

// History is the optional context-engineering layer for agentcore runtimes.
//
// Storage remains the authority for resumable run state and full transcripts.
// History is a derived layer that can maintain summaries, windows, retrieval
// indexes, or other context-oriented views over committed transcript messages.
type History interface {
	Hydrate(ctx context.Context, state *RunState) error
	ObserveMessages(ctx context.Context, state *RunState, messages ...Message) error
	BuildContext(ctx context.Context, state *RunState) ([]Message, error)
}
