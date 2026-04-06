package agentcore

import "context"

// Memory is the optional context-engineering layer for agentcore runtimes.
//
// Storage remains the authority for resumable run state and full transcripts.
// Memory is a derived layer that can maintain summaries, windows, retrieval
// indexes, or other context-oriented views over committed transcript messages.
type Memory interface {
	Hydrate(ctx context.Context, state *RunState) error
	ObserveMessages(ctx context.Context, state *RunState, messages ...Message) error
	BuildContext(ctx context.Context, state *RunState) ([]Message, error)
}

type noopMemory struct{}

func (noopMemory) Hydrate(context.Context, *RunState) error { return nil }

func (noopMemory) ObserveMessages(context.Context, *RunState, ...Message) error { return nil }

func (noopMemory) BuildContext(_ context.Context, state *RunState) ([]Message, error) {
	if state == nil || len(state.Messages) == 0 {
		return nil, nil
	}
	return cloneMessagesValue(state.Messages), nil
}
