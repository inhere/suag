package personal

import (
	"context"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore/mock"
)

// TestRunRecoversStaleRunningSession covers a session left in running by a
// process killed mid-turn (kill -9, a crash): its next turn recovers it when
// this process can prove no live process is running it, and is refused as
// before when it cannot.
func TestRunRecoversStaleRunningSession(t *testing.T) {
	ctx := context.Background()
	newRunner := func(t *testing.T, repo *store.Store, lockRoot string) *Runner {
		t.Helper()
		runner, err := New(Config{Repository: repo, Provider: mock.NewProvider(mock.Final("回答")),
			Model: "m", ProviderName: "p", LockRoot: lockRoot})
		assert.NoErr(t, err)
		return runner
	}
	// create leaves s1 running with turn t1 that never ended, the state
	// markRunning wrote before the process died. A turnStatus of "" leaves the
	// session without a last turn.
	create := func(t *testing.T, repo *store.Store, meta map[string]any, turnStatus string) {
		t.Helper()
		assert.NoErr(t, repo.CreateSession(ctx, store.Session{ID: "s1", Status: store.SessionOpen, Meta: meta}))
		if turnStatus != "" {
			assert.NoErr(t, repo.CreateTurn(ctx, store.Turn{ID: "t1", SessionID: "s1", Status: turnStatus}))
		}
		sess, err := repo.GetSession(ctx, "s1")
		assert.NoErr(t, err)
		sess.Status = store.SessionRunning
		if turnStatus != "" {
			sess.LastTurnID = "t1"
		}
		assert.NoErr(t, repo.UpdateSessionFields(ctx, sess))
	}
	session := func(t *testing.T, repo *store.Store) Session {
		t.Helper()
		sess, err := repo.GetSession(ctx, "s1")
		assert.NoErr(t, err)
		return sess
	}
	turn := func(t *testing.T, repo *store.Store) store.Turn {
		t.Helper()
		got, err := repo.GetTurn(ctx, "t1")
		assert.NoErr(t, err)
		return got
	}
	// recovered asserts what a recovery leaves behind: the session is open and
	// the new turn became its last one.
	recovered := func(t *testing.T, repo *store.Store, res RunResult) {
		t.Helper()
		assert.Eq(t, store.SessionOpen, session(t, repo).Status)
		assert.Eq(t, res.TurnID, session(t, repo).LastTurnID)
	}
	// traceEvents asserts the recorded "session recovered" event (one per
	// recovery) and returns it; wantTurn is its turn ("" when the session had
	// no last turn).
	traceEvents := func(t *testing.T, repo *store.Store, wantTurn string) store.Event {
		t.Helper()
		events := eventsOf(t, repo, "s1", store.EventSession)
		assert.Require(t, assert.Eq(t, 1, len(events)))
		assert.Eq(t, "recovered", events[0].Name)
		assert.Eq(t, wantTurn, events[0].TurnID)
		assert.Eq(t, store.SessionRunning, events[0].Attrs["previous_status"])
		return events[0]
	}
	// runTurn runs one prompt on the session and reports what the caller saw.
	runTurn := func(t *testing.T, runner *Runner, reqCtx context.Context) (RunResult, bool, error) {
		t.Helper()
		recoveredEvent := false
		res, err := runner.Run(reqCtx, RunRequest{SessionID: "s1", Prompt: "继续", Emit: func(_ context.Context, e Event) error {
			if e.Kind == EventSessionRecovered {
				recoveredEvent = true
			}
			return nil
		}})
		return res, recoveredEvent, err
	}

	t.Run("chat holds the lock: the session is recovered and the turn runs", func(t *testing.T) {
		repo := openObserveRepo(t)
		runner := newRunner(t, repo, t.TempDir())
		create(t, repo, nil, store.TurnRunning)
		release, err := runner.HoldSessionLock("s1") // chat holds it for its whole lifetime
		assert.NoErr(t, err)
		defer func() { _ = release() }()

		res, recoveredEvent, err := runTurn(t, runner, ctx)
		assert.NoErr(t, err)
		assert.True(t, recoveredEvent)
		assert.Eq(t, "回答", res.Output)
		recovered(t, repo, res)
		// The turn the dead process was running is finished as cancelled, like a
		// turn interrupted at an approval question, and keeps its messages.
		dead := turn(t, repo)
		assert.Eq(t, store.TurnCancelled, dead.Status)
		assert.Eq(t, turnStopStaleProcess, dead.StopReason)
		assert.False(t, dead.EndedAt.IsZero())
		traceEvents(t, repo, "t1")
	})

	t.Run("the turn takes the lock itself", func(t *testing.T) {
		repo := openObserveRepo(t)
		runner := newRunner(t, repo, t.TempDir())
		create(t, repo, nil, store.TurnRunning)

		res, recoveredEvent, err := runTurn(t, runner, ctx)
		assert.NoErr(t, err)
		assert.True(t, recoveredEvent)
		recovered(t, repo, res)
		assert.Eq(t, store.TurnCancelled, turn(t, repo).Status)
		traceEvents(t, repo, "t1")
	})

	t.Run("another process holds the lock: no recovery, still refused", func(t *testing.T) {
		lockRoot := t.TempDir()
		repo := openObserveRepo(t)
		runner := newRunner(t, repo, lockRoot)
		other := newRunner(t, repo, lockRoot) // its own heldLocks, like another process
		create(t, repo, nil, store.TurnRunning)
		release, err := other.HoldSessionLock("s1")
		assert.NoErr(t, err)
		defer func() { _ = release() }()

		// Run waits up to 30s for the lock of a live holder; the caller's ctx
		// ends that wait.
		waitCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		defer cancel()
		_, recoveredEvent, err := runTurn(t, runner, waitCtx)
		assert.Err(t, err)
		assert.False(t, recoveredEvent)
		assert.Eq(t, store.SessionRunning, session(t, repo).Status)
		assert.Eq(t, store.TurnRunning, turn(t, repo).Status)
		assert.Eq(t, 0, len(eventsOf(t, repo, "s1", store.EventSession)))
		turns, listErr := repo.ListTurns(ctx, "s1")
		assert.NoErr(t, listErr)
		assert.Eq(t, 1, len(turns)) // no new turn
	})

	t.Run("no cross-process lock: no proof, still refused", func(t *testing.T) {
		repo := openObserveRepo(t)
		runner := newRunner(t, repo, "")
		create(t, repo, nil, store.TurnRunning)

		_, recoveredEvent, err := runTurn(t, runner, ctx)
		assert.Err(t, err)
		assert.Contains(t, err.Error(), "not ready for a new turn: running")
		assert.False(t, recoveredEvent)
		assert.Eq(t, store.SessionRunning, session(t, repo).Status)
		assert.Eq(t, 0, len(eventsOf(t, repo, "s1", store.EventSession)))
	})

	t.Run("a sub-agent session is left alone", func(t *testing.T) {
		// Sub-agent runners are built without LockRoot (wiring), so a live
		// sub-agent turn holds no lock and holding it proves nothing here.
		repo := openObserveRepo(t)
		runner := newRunner(t, repo, t.TempDir())
		create(t, repo, map[string]any{store.MetaKind: store.KindAgent}, store.TurnRunning)
		release, err := runner.HoldSessionLock("s1")
		assert.NoErr(t, err)
		defer func() { _ = release() }()

		_, recoveredEvent, err := runTurn(t, runner, ctx)
		assert.Err(t, err)
		assert.False(t, recoveredEvent)
		assert.Eq(t, store.SessionRunning, session(t, repo).Status)
		assert.Eq(t, store.TurnRunning, turn(t, repo).Status)
	})

	t.Run("the last turn already ended: only the status is reset", func(t *testing.T) {
		repo := openObserveRepo(t)
		runner := newRunner(t, repo, t.TempDir())
		create(t, repo, nil, store.TurnCompleted)

		res, recoveredEvent, err := runTurn(t, runner, ctx)
		assert.NoErr(t, err)
		assert.True(t, recoveredEvent)
		recovered(t, repo, res)
		ended := turn(t, repo) // left as it was: only a running turn is finished
		assert.Eq(t, store.TurnCompleted, ended.Status)
		assert.Eq(t, "", ended.StopReason)
		traceEvents(t, repo, "t1")
	})

	t.Run("no last turn: only the status is reset", func(t *testing.T) {
		repo := openObserveRepo(t)
		runner := newRunner(t, repo, t.TempDir())
		create(t, repo, nil, "")

		res, recoveredEvent, err := runTurn(t, runner, ctx)
		assert.NoErr(t, err)
		assert.True(t, recoveredEvent)
		recovered(t, repo, res)
		traceEvents(t, repo, "")
	})

	t.Run("the turn ended after the read that found running", func(t *testing.T) {
		// That read happened before the lock was taken: by then another process
		// may have finished its turn (and released the lock). With the lock held
		// nothing else can set the status, so a second read decides - and here it
		// finds nothing to recover.
		repo := openObserveRepo(t)
		runner := newRunner(t, repo, t.TempDir())
		create(t, repo, nil, store.TurnCompleted)
		staleSnapshot := Session{ID: "s1", Status: store.SessionRunning, LastTurnID: "t1"}
		sess := session(t, repo) // the other process finished the turn meanwhile
		sess.Status = store.SessionOpen
		assert.NoErr(t, repo.UpdateSessionFields(ctx, sess))

		got, err := runner.recoverStaleSession(ctx, staleSnapshot, true, nil)
		assert.NoErr(t, err)
		assert.Eq(t, store.SessionOpen, got.Status)
		assert.Eq(t, 0, len(eventsOf(t, repo, "s1", store.EventSession)))
		assert.Eq(t, store.TurnCompleted, turn(t, repo).Status)
	})

	t.Run("a session that is not running is untouched", func(t *testing.T) {
		repo := openObserveRepo(t)
		runner := newRunner(t, repo, t.TempDir())
		assert.NoErr(t, repo.CreateSession(ctx, store.Session{ID: "s1", Status: store.SessionOpen}))

		res, recoveredEvent, err := runTurn(t, runner, ctx)
		assert.NoErr(t, err)
		assert.False(t, recoveredEvent)
		recovered(t, repo, res)
		assert.Eq(t, 0, len(eventsOf(t, repo, "s1", store.EventSession)))
	})
}
