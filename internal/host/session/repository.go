package session

import (
	"context"
	"time"
)

// Repository persists Suag Host session lifecycle state.
//
// It is the product-layer authority for sessions, turns, runs, events,
// external tasks, recaps, and memory candidates. It is intentionally separate
// from agentcore.Storage, which only persists single-runtime run snapshots and
// transcripts.
type Repository interface {
	CreateSession(ctx context.Context, session Session) error
	GetSession(ctx context.Context, sessionID string) (Session, error)
	ListSessions(ctx context.Context) ([]Session, error)
	UpdateSession(ctx context.Context, session Session) error
	CreateTurn(ctx context.Context, turn Turn) error
	UpdateTurnStatus(ctx context.Context, turnID string, status TurnStatus, completedAt *time.Time) error
	CreateRun(ctx context.Context, run Run) error
	UpdateRunLifecycle(ctx context.Context, runID string, state RunState, summary string, failureCode string, endedAt *time.Time) error
	UpdateRunMetadata(ctx context.Context, runID string, metadata map[string]any) error
	GetRun(ctx context.Context, runID string) (Run, error)
	AppendEvent(ctx context.Context, event RunEvent) (RunEvent, error)
	ListEventsBySession(ctx context.Context, sessionID string, afterSequence int64) ([]RunEvent, error)
	CreateExternalTask(ctx context.Context, task ExternalTask) error
	UpdateExternalTask(ctx context.Context, task ExternalTask) error
	ListExternalTasksBySession(ctx context.Context, sessionID string) ([]ExternalTask, error)
	ListExternalTasksByRun(ctx context.Context, runID string) ([]ExternalTask, error)
	CreateRecap(ctx context.Context, recap Recap) error
	ListRecapsByTurn(ctx context.Context, sessionID string, turnID string) ([]Recap, error)
	CreateMemoryCandidate(ctx context.Context, candidate MemoryCandidate) error
	GetMemoryCandidate(ctx context.Context, candidateID string) (MemoryCandidate, error)
	ListMemoryCandidatesBySession(ctx context.Context, sessionID string) ([]MemoryCandidate, error)
	ListMemoryCandidatesByTurn(ctx context.Context, sessionID string, turnID string) ([]MemoryCandidate, error)
	UpdateMemoryCandidateStatus(ctx context.Context, candidateID string, status MemoryCandidateStatus) error
	Close() error
}
