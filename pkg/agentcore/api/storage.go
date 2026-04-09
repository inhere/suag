package api

import "context"

// Storage persists and reloads public run state snapshots.
type Storage interface {
	LoadRun(ctx context.Context, runID string) (*RunState, error)
	LoadMessages(ctx context.Context, runID string) ([]Message, error)
	SaveRun(ctx context.Context, state *RunState) error
	AppendMessages(ctx context.Context, runID string, msgs ...Message) error
}

// ArtifactStorage is an optional storage capability for artifact persistence and replay.
type ArtifactStorage interface {
	SaveArtifacts(ctx context.Context, runID string, artifacts []Artifact) error
	LoadArtifacts(ctx context.Context, runID string) ([]Artifact, error)
}
