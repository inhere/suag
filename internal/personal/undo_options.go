package personal

import (
	"context"
	"fmt"
)

// UndoOptions selects the checkpoints an undo operation may restore.
// TurnID takes precedence over Count when it is set; Force only covers content
// conflicts and never bypasses the other safety checks.
type UndoOptions struct {
	TurnID string
	Count  int
	Force  bool
}

// UndoWithOptions restores selected checkpoints and queues the same deferred
// notification used by chat. It keeps the CLI provider-independent while
// preserving the existing lock and safety checks in UndoCore.
func (u *UndoCore) UndoWithOptions(ctx context.Context, sessionQuery string, options UndoOptions) (UndoResult, error) {
	var result UndoResult
	if err := u.validate(); err != nil {
		return result, err
	}
	sessionID, err := ResolveSessionID(ctx, u.Repository, sessionQuery)
	if err != nil {
		return result, err
	}
	result.SessionID = sessionID
	result.Forced = options.Force
	err = u.withSession(ctx, sessionID, func() error {
		session, err := u.Repository.GetSession(ctx, sessionID)
		if err != nil {
			return err
		}
		rows, err := u.checkpointsLocked(ctx, sessionID, 0)
		if err != nil {
			return err
		}
		selected := selectUndoRowsForOptions(rows, options.Count, options.TurnID)
		for _, row := range selected {
			if !row.HasSnapshots() {
				continue
			}
			result.Turns++
			fullyUndone := true
			// Snapshot calls are persisted in call order; undo visits them in
			// reverse order so repeated writes converge on the original content.
			for i := len(row.Items) - 1; i >= 0; i-- {
				item := row.Items[i]
				outcome, restoreErr := u.restoreItem(ctx, session, sessionID, row.TurnID, item, options.Force)
				if outcome.action == CheckpointActionRestore || outcome.action == CheckpointActionDelete {
					result.Files = append(result.Files, outcome.path)
				}
				switch outcome.action {
				case CheckpointActionRestore:
					result.Restored++
				case CheckpointActionDelete:
					result.Deleted++
				case CheckpointActionUnrecoverable:
					result.Unrecoverable++
				default:
					result.Skipped++
				}
				if restoreErr != nil {
					result.Failures = append(result.Failures, restoreErr.Error())
					fullyUndone = false
				}
				if outcome.unresolved {
					fullyUndone = false
				}
			}
			state := SnapshotStatePartial
			if fullyUndone {
				state = SnapshotStateUndone
			}
			if err := u.Snapshots.SetCheckpointState(sessionID, row.TurnID, state); err != nil {
				return fmt.Errorf("set checkpoint %s state: %w", row.TurnID, err)
			}
		}
		// Keep the session lock until the deferred fact is queued, so a new
		// turn cannot start between restoration and notification.
		if result.Turns > 0 {
			if err := u.enqueueUndoNotice(ctx, result); err != nil {
				return fmt.Errorf("queue undo notice: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	return result, nil
}

func selectUndoRowsForOptions(rows []Checkpoint, count int, turnID string) []Checkpoint {
	if turnID != "" {
		for _, row := range rows {
			if row.TurnID == turnID && row.State != SnapshotStateUndone {
				return []Checkpoint{row}
			}
		}
		return nil
	}
	if count <= 0 {
		count = 1
	}
	selected := make([]Checkpoint, 0, count)
	fileTurns := 0
	for _, row := range rows {
		if row.IsShellOnly() {
			selected = append(selected, row)
			continue
		}
		if row.State == SnapshotStateUndone {
			continue
		}
		selected = append(selected, row)
		fileTurns++
		if fileTurns == count {
			break
		}
	}
	return selected
}
