package wiring

import (
	"context"
	"fmt"

	appconfig "github.com/inhere/suga/internal/config"
	"github.com/inhere/suga/internal/personal"
	"github.com/inhere/suga/pkg/agentcore/api"
	"github.com/inhere/suga/pkg/agentcore/tools/native"
)

const snapshotUnrecoverableError = "snapshot_error"

// policySnapshotter adapts the storage error policy to native's small hook
// contract. deny returns an error before I/O; continue deliberately returns a
// visible unrecoverable result so a write is never presented as undoable.
type policySnapshotter struct {
	store       *personal.SessionSnapshots
	onError     string
	unavailable error
}

var _ native.Snapshotter = (*policySnapshotter)(nil)

func (s *policySnapshotter) BeforeCall(ctx context.Context, req native.SnapshotRequest) (native.SnapshotBefore, error) {
	// Tools assembled for catalog/approval tests may be called outside a runner;
	// only a session-scoped call can produce a useful checkpoint.
	if api.SessionIDFrom(ctx) == "" {
		return native.SnapshotBefore{}, nil
	}
	if s.unavailable != nil {
		return s.unavailableBefore()
	}
	before, err := s.store.BeforeCall(ctx, req)
	if err == nil || s.onError != appconfig.SnapshotOnErrorContinue {
		return before, err
	}
	return native.SnapshotBefore{
		Unrecoverable: snapshotUnrecoverableError,
		Notice:        "本次写入不可撤销：快照不可用（" + err.Error() + "）",
	}, nil
}

func (s *policySnapshotter) AfterCall(ctx context.Context, result native.SnapshotResult) error {
	if api.SessionIDFrom(ctx) == "" || s.unavailable != nil || s.store == nil {
		return nil
	}
	err := s.store.AfterCall(ctx, result)
	if err != nil && s.onError == appconfig.SnapshotOnErrorContinue {
		return nil
	}
	return err
}

func (s *policySnapshotter) unavailableBefore() (native.SnapshotBefore, error) {
	if s.onError == appconfig.SnapshotOnErrorContinue {
		return native.SnapshotBefore{
			Unrecoverable: snapshotUnrecoverableError,
			Notice:        "本次写入不可撤销：快照不可用（" + s.unavailable.Error() + "）",
		}, nil
	}
	return native.SnapshotBefore{}, s.unavailable
}

// configuredSnapshotter builds the one policy used by every native tool catalog
// assembled in this process. Cleanup runs before the catalog is exposed, so an
// uncertain retention scan is fail-closed for the next write by default.
func configuredSnapshotter() native.Snapshotter {
	settings, err := appconfig.ReadSnapshotSettings()
	if err != nil {
		return &policySnapshotter{onError: appconfig.SnapshotOnErrorDeny, unavailable: err}
	}
	if !settings.Enabled {
		return nil
	}
	sessionsDir, err := appconfig.ResolveSessionsDir()
	if err != nil {
		return &policySnapshotter{onError: settings.OnError, unavailable: err}
	}
	store := personal.NewSessionSnapshotsWithLimits(sessionsDir, personal.SnapshotLimits{
		MaxFileBytes: settings.MaxFileBytes, MaxSessionBytes: settings.MaxSessionBytes,
		MaxGlobalBytes: settings.MaxGlobalBytes, KeepTurns: settings.KeepTurns, KeepDays: settings.KeepDays,
	})
	if _, err := store.CleanupAll(); err != nil {
		return &policySnapshotter{store: store, onError: settings.OnError, unavailable: fmt.Errorf("snapshot cleanup: %w", err)}
	}
	return &policySnapshotter{store: store, onError: settings.OnError}
}
