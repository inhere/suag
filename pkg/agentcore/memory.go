package agentcore

import (
	"context"

	"github.com/inhere/axon/pkg/agentcore/api"
)

type Memory = api.Memory

type noopMemory struct{}

func (noopMemory) Hydrate(context.Context, *RunState) error { return nil }

func (noopMemory) ObserveMessages(context.Context, *RunState, ...Message) error { return nil }

func (noopMemory) BuildContext(_ context.Context, state *RunState) ([]Message, error) {
	if state == nil || len(state.Messages) == 0 {
		return nil, nil
	}
	return cloneMessagesValue(state.Messages), nil
}
