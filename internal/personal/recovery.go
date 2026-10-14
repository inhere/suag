package personal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/inhere/suga/internal/personal/store"
)

// Session recovery. Runner.markRunning marks a session running when one of its
// turns starts and only Runner.finish takes it back to open, so a process
// killed mid-turn (kill -9, a crash, a power loss) leaves its session in
// running for good: every later turn of that session is refused with
// "not ready for a new turn: running", which is what a chat continued with
// --resume (or run --resume) hits. recoverStaleSession takes such a session
// back to open so the next turn can start.
//
// Recovering is only safe when no live process is inside a turn of the session.
// The proof used here is the session's cross-process lock (process.SessionLock):
// every writer takes it before it touches the session - chat holds it for its
// whole lifetime (Runner.HoldSessionLock), run and ACP take it for the turn
// (Runner.Run) - and the OS releases it when its holder dies. Holding the lock
// therefore means the running status cannot come from a live process: it is the
// leftover of one that died. Two cases are no proof at all and are left alone,
// so the caller keeps refusing (both are reported there):
//
//   - Config.LockRoot is empty: no cross-process lock is taken anywhere, so
//     another process may well be inside a turn of the session.
//   - a sub-agent session (store.KindAgent): its runner is built without
//     LockRoot (wiring.subAgents), so a live sub-agent turn holds no lock and
//     holding it here proves nothing about that session.
//
// The limit of the proof: it trusts a lock that is actually taken, i.e. the
// lock directory (LockRoot) still existing and the session being written by a
// suga that uses it.
const (
	// EventSessionRecovered is the caller-facing event kind of a recovered
	// session. Chat renders SessionRecoveredNotice for it and run prints the
	// same notice on stderr; it has no stream-json line, so a consumer that does
	// not know it ignores it.
	EventSessionRecovered = "session_recovered"
	// SessionRecoveredNotice is the one-line notice a caller shows when a
	// session left running by a dead process was taken back to open.
	SessionRecoveredNotice = "上一回合异常中断，已恢复会话（中断的回合记为 cancelled）"
	// turnStopStaleProcess is the stop reason of a turn that was still running
	// when a recovery found the process running it gone (the turn status is
	// store.TurnCancelled, like a turn interrupted at an approval question).
	turnStopStaleProcess = "stale_process"
	// recoveredEventName is the name of the trace event of a recovery:
	// "session recovered".
	recoveredEventName = "recovered"
)

// recoverStaleSession takes a session left in running by a dead process back to
// open and records the turn that was running as cancelled, so the next turn can
// start. lockHeld reports whether this process holds the session's
// cross-process lock (it took it for this turn, or chat holds it for its whole
// lifetime), which is the proof described above; without it nothing is changed
// and false is returned.
//
// It returns the session (status open once recovered), whether it recovered it,
// and an error when the store could not be written. The recovery is
// observable: a store event in the trace (kind store.EventSession, name
// "recovered") and the EventSessionRecovered notice for the caller; a failure
// of either is logged and never fails the turn.
func (r *Runner) recoverStaleSession(ctx context.Context, model Session, lockHeld bool, sink EmitFunc) (Session, bool, error) {
	if model.Status != store.SessionRunning || !lockHeld || store.IsAgentSession(model) {
		return model, false, nil
	}
	now := time.Now().UTC()
	// The turn the dead process was running: markRunning wrote it as the
	// session's last turn and left it running. Only a turn that is still
	// running is finished; an older session may point at one that ended long
	// ago, and that status is kept as it is.
	turnID, turnStatus := model.LastTurnID, ""
	if turnID != "" {
		turn, err := r.cfg.Repository.GetTurn(ctx, turnID)
		switch {
		case errors.Is(err, store.ErrTurnNotFound):
			// Nothing to finish: the row is gone (gc) or was never written.
		case err != nil:
			return model, false, fmt.Errorf("recover session %q: read turn %q: %w", model.ID, turnID, err)
		case turn.Status == store.TurnRunning:
			// Cancelled, the terminal state of a turn that did not end: what it
			// managed to write stays as history it never answered (the same
			// status a turn interrupted at an approval question gets).
			ended := store.Turn{ID: turnID, Status: store.TurnCancelled, StopReason: turnStopStaleProcess, EndedAt: now}
			if err := r.cfg.Repository.UpdateTurn(ctx, ended); err != nil {
				return model, false, fmt.Errorf("recover session %q: cancel turn %q: %w", model.ID, turnID, err)
			}
			turnStatus = store.TurnCancelled
		}
	}
	attrs := map[string]any{"previous_status": store.SessionRunning}
	if !model.UpdatedAt.IsZero() {
		// How long the session sat in running, i.e. since markRunning wrote it.
		attrs["stale_ms"] = now.Sub(model.UpdatedAt).Milliseconds()
	}
	if turnStatus != "" {
		attrs["turn_status"] = turnStatus
	}
	// The status comes first: should this process die between the two writes,
	// the session is already usable again (the event is only an observation).
	model.Status, model.UpdatedAt = store.SessionOpen, now
	if err := r.cfg.Repository.UpdateSessionFields(ctx, model); err != nil {
		return model, false, fmt.Errorf("recover session %q: %w", model.ID, err)
	}
	// The scope is set here because the turn scope of the caller's ctx may
	// belong to another turn (a sub-agent run inherits its parent's).
	r.rec.record(withObserveScope(ctx, model.ID, turnID), store.Event{
		SessionID: model.ID, TurnID: turnID, Kind: store.EventSession, Name: recoveredEventName,
		Status: eventOK, StartedAt: now, Attrs: attrs,
	})
	if err := emit(ctx, sink, Event{Kind: EventSessionRecovered, SessionID: model.ID, TurnID: turnID}); err != nil {
		slog.Warn("emit recovered session", "session_id", model.ID, "err", err)
	}
	return model, true, nil
}
