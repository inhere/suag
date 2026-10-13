package chat

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"

	sugahooks "github.com/inhere/suga/internal/hooks"
	"github.com/inhere/suga/pkg/agentcore/tasks"
)

// gatedStops answers each Stop with the next value sent on replies, or with
// the cancellation error when the Stop's context ends first.
type gatedStops struct {
	sugahooks.Client
	started chan sugahooks.Event
	replies chan sugahooks.Result
}

func (g *gatedStops) StartStop(ctx context.Context, event sugahooks.Event) sugahooks.Wait {
	g.started <- event
	done := make(chan sugahooks.Result, 1)
	go func() {
		select {
		case r := <-g.replies:
			done <- r
		case <-ctx.Done():
			done <- sugahooks.Result{Err: ctx.Err()}
		}
		close(done)
	}()
	return &fakeStopWait{done: done, interrupt: func() {}}
}

func newBackgroundStop(t *testing.T) (*backgroundStop, *gatedStops) {
	t.Helper()
	dir := t.TempDir()
	m := tasks.NewManager(tasks.Config{Output: func(sid, id string) (string, string, error) {
		return filepath.Join(dir, id+".txt"), "out:" + sid + ":" + id, nil
	}})
	t.Cleanup(m.Close)
	hooks := &gatedStops{started: make(chan sugahooks.Event, 4), replies: make(chan sugahooks.Result)}
	return &backgroundStop{hooks: hooks, tasks: m, sessionID: "s1"}, hooks
}

// waitStatus waits until the task has the status.
func waitStatus(t *testing.T, m *tasks.Manager, id string, want tasks.Status) tasks.Info {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if info, _ := m.Get("s1", id); info.Status == want {
			return info
		}
	}
	info, _ := m.Get("s1", id)
	t.Fatalf("task %s is %s, want %s", id, info.Status, want)
	return info
}

func TestBackgroundStop(t *testing.T) {
	t.Run("a block reply is a notice and marks the next turn", func(t *testing.T) {
		b, hooks := newBackgroundStop(t)
		assert.Require(t, assert.NoErr(t, b.start(context.Background(), sugahooks.Event{Name: "Stop"})))
		<-hooks.started
		hooks.replies <- sugahooks.Result{Response: sugahooks.Response{Decision: "block", Reason: "fix the failing test"}}
		info := waitStatus(t, b.tasks, b.current, tasks.Exited)
		assert.Eq(t, "hook", info.Kind)
		got := b.tasks.TakeNotices("s1")
		assert.Require(t, assert.Len(t, got, 1)) // no end notice besides it
		assert.Eq(t, "Stop hook asks to continue: fix the failing test", got[0].Notice)
		assert.True(t, b.takeBlocked())
		assert.False(t, b.takeBlocked())
	})
	t.Run("a newer Stop kills the running one", func(t *testing.T) {
		b, hooks := newBackgroundStop(t)
		assert.Require(t, assert.NoErr(t, b.start(context.Background(), sugahooks.Event{Name: "Stop"})))
		<-hooks.started
		first := b.current
		assert.Require(t, assert.NoErr(t, b.start(context.Background(), sugahooks.Event{Name: "Stop", StopHookActive: true})))
		<-hooks.started
		assert.Eq(t, tasks.Killed, waitStatus(t, b.tasks, first, tasks.Killed).Status)
		assert.NotEq(t, first, b.current)
		hooks.replies <- sugahooks.Result{Response: sugahooks.Response{Decision: "approve"}}
		waitStatus(t, b.tasks, b.current, tasks.Exited)
		assert.Empty(t, b.tasks.TakeNotices("s1")) // no block, no notice
		assert.False(t, b.takeBlocked())
	})
	t.Run("a nil backgroundStop is never blocked", func(t *testing.T) {
		var b *backgroundStop
		assert.False(t, b.takeBlocked())
	})
}
