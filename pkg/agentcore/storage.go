package agentcore

import "context"

// Storage persists and reloads public run state snapshots.
type Storage interface {
	SaveRun(ctx context.Context, record *RunRecord) error
	LoadRun(ctx context.Context, runID string) (*RunRecord, error)
}
