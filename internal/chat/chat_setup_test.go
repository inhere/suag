package chat

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/pkg/agentcore/mock"
)

// TestPersonalChatSetupSession covers the hook the CLI publishes the session's
// added directories through (--add-dir, wiring.Runtime.ApplyAddDirs): it runs
// once the session is open, before the first turn, and a failure stops the chat.
func TestPersonalChatSetupSession(t *testing.T) {
	runner, _ := newCLITestRunner(t, mock.NewProvider(mock.Final("done")))
	ctx := context.Background()

	t.Run("it is called with the opened session", func(t *testing.T) {
		var got string
		calls := 0
		err := Run(ctx, runner, Options{SessionID: "adddir-chat-1", SetupSession: func(_ context.Context, sessionID string) error {
			calls++
			got = sessionID
			return nil
		}}, strings.NewReader(""), io.Discard, io.Discard, nil)
		assert.NoErr(t, err)
		assert.Eq(t, 1, calls)
		assert.Eq(t, "adddir-chat-1", got)
	})
	t.Run("a failure stops the chat", func(t *testing.T) {
		err := Run(ctx, runner, Options{SessionID: "adddir-chat-2", SetupSession: func(context.Context, string) error {
			return errors.New("added directory refused")
		}}, strings.NewReader("prompt\n"), io.Discard, io.Discard, nil)
		assert.ErrMsgContains(t, err, "added directory refused")
	})
	t.Run("a run without the hook is unaffected", func(t *testing.T) {
		assert.NoErr(t, Run(ctx, runner, Options{}, strings.NewReader(""), io.Discard, io.Discard, nil))
	})
}
