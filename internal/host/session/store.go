package session

import (
	"context"
	"time"
)

type Store interface {
	CreateSession(ctx context.Context, session Session) error
	GetSession(ctx context.Context, sessionID string) (Session, error)
	CreateTurn(ctx context.Context, turn Turn) error
	CreateRun(ctx context.Context, run Run) error
	UpdateRunState(ctx context.Context, runID string, state RunState, endedAt *time.Time) error
	AppendEvent(ctx context.Context, event RunEvent) error
	ListEventsBySession(ctx context.Context, sessionID string, afterSequence int64) ([]RunEvent, error)
	Close() error
}
