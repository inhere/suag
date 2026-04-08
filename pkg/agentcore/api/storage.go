package api

import "context"

// Storage persists and reloads public run state snapshots.
type Storage interface {
	LoadRun(ctx context.Context, runID string) (*RunState, error)
	LoadMessages(ctx context.Context, runID string) ([]Message, error)
	SaveRun(ctx context.Context, state *RunState) error
	AppendMessages(ctx context.Context, runID string, msgs ...Message) error
}
