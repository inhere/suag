package chat

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"

	sugahooks "github.com/inhere/suga/internal/hooks"
	"github.com/inhere/suga/pkg/agentcore/api"
	"github.com/inhere/suga/pkg/agentcore/tasks"
)

// backgroundStop runs each turn's Stop hook as a background task
// (hooks.stop_background) instead of a wait at the prompt: typing, deliveries
// and task wakes no longer interrupt it, so a slow Stop (a full test run, a
// wait for a reply from elsewhere) finishes. A block reply becomes a notice of
// the task: an idle chat wakes for it (tasks.wake_on_exit), a running turn gets
// it before its next model request. A newer Stop kills the previous one, as
// only the latest turn's state matters.
type backgroundStop struct {
	hooks     sugahooks.Client
	tasks     *tasks.Manager
	sessionID string

	mu      sync.Mutex
	current string // id of the running Stop task
	// blocked: a Stop asked to continue; the next turn's Stop then carries
	// stop_hook_active, as with a Stop wait.
	blocked atomic.Bool
}

// start runs the Stop event in the background, killing a previous one still
// running. A failure to start is returned (the caller falls back to a wait).
func (b *backgroundStop) start(ctx context.Context, stop sugahooks.Event) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.current != "" {
		if info, err := b.tasks.Get(b.sessionID, b.current); err == nil && info.Status == tasks.Running {
			_, _ = b.tasks.Kill(b.sessionID, b.current)
		}
		b.current = ""
	}
	info, err := b.tasks.Start(api.WithSessionID(ctx, b.sessionID), tasks.Spec{
		Kind: "hook", Summary: "Stop hook", NoEndNotice: true,
		Run: func(ctx context.Context, out io.Writer, r tasks.Reporter) (int, error) {
			return b.run(ctx, stop, out, r)
		},
	})
	if err != nil {
		return err
	}
	b.current = info.ID
	return nil
}

// run is the Stop task: it calls the hook (the recording client traces it as
// a hook event) and turns a block reply into a notice.
func (b *backgroundStop) run(ctx context.Context, stop sugahooks.Event, out io.Writer, r tasks.Reporter) (int, error) {
	result := <-b.hooks.StartStop(ctx, stop).Done()
	if notice := strings.TrimSpace(result.Response.Notice); notice != "" {
		fmt.Fprintln(out, "notice: "+notice)
	}
	if result.Err != nil {
		if ctx.Err() != nil {
			return -1, nil // killed by a newer Stop or chat's exit
		}
		return -1, result.Err
	}
	reason := strings.TrimSpace(result.Response.Reason)
	fmt.Fprintf(out, "decision: %s\n", cmp.Or(result.Response.Decision, "none"))
	if result.Response.Decision == "block" && reason != "" {
		fmt.Fprintln(out, "reason: "+reason)
		b.blocked.Store(true)
		r.Notify("Stop hook asks to continue: " + reason)
	}
	return 0, nil
}

// takeBlocked reports (once) that a Stop asked to continue.
func (b *backgroundStop) takeBlocked() bool { return b != nil && b.blocked.Swap(false) }
