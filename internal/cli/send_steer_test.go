package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal/process"
)

// Tests of `suga send --steer`: the request carries the mode the server resolves,
// the command says what became of the message, and a discarded one exits 3.

// steerSendHarness runs `suga send` against a chat process whose delivery endpoint
// answers like chatState.deliverMessage: it records what it was asked for
// ("<mode>:<from>:<text>") and answers what the test set up.
type steerSendHarness struct {
	session string
	asked   chan string
}

func newSteerSendHarness(t *testing.T, sessionID string, answer func() (process.Delivery, <-chan error, error)) *steerSendHarness {
	t.Helper()
	home := shortCLIHome(t)
	t.Setenv("SUGA_HOME", home)
	asked := make(chan string, 8)
	server, err := process.StartWait(filepath.Join(home, "run"), sessionID, "steer", t.TempDir(),
		func(mode, text, from string) (process.Delivery, <-chan error, error) {
			asked <- mode + ":" + from + ":" + text
			return answer()
		})
	assert.Require(t, assert.NoErr(t, err))
	t.Cleanup(func() { _ = server.Close() })
	return &steerSendHarness{session: sessionID, asked: asked}
}

// run executes `suga send --session <session> <args...>` and returns its stderr.
func (h *steerSendHarness) run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmdArgs := append([]string{"send", "--session", h.session}, args...)
	err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs(cmdArgs)
	return stderr.String(), err
}

// steered answers with the acknowledgement of a message inserted into the turn
// running right now; fate is what its sender is told afterwards.
func steered(fate error) func() (process.Delivery, <-chan error, error) {
	return func() (process.Delivery, <-chan error, error) {
		settled := make(chan error, 1)
		settled <- fate
		return process.Delivery{Steered: true}, settled, nil
	}
}

// queued answers with the acknowledgement of a message waiting for its own turn.
func queued(fate error) func() (process.Delivery, <-chan error, error) {
	return func() (process.Delivery, <-chan error, error) {
		settled := make(chan error, 1)
		settled <- fate
		return process.Delivery{Queued: true}, settled, nil
	}
}

func TestSendSteerAsksForTheRunningTurn(t *testing.T) {
	h := newSteerSendHarness(t, "session-steer-a", steered(nil))
	stderr, err := h.run(t, "--steer", "只改 README")
	assert.NoErr(t, err)
	assert.Eq(t, "steer:cli:只改 README", <-h.asked)
	assert.Eq(t, "steered\n", stderr)
}

func TestSendSteerReportsWhatItWaitedFor(t *testing.T) {
	t.Run("a steered message with --wait reports delivered", func(t *testing.T) {
		h := newSteerSendHarness(t, "session-steer-b", steered(nil))
		stderr, err := h.run(t, "--steer", "--wait", "hi")
		assert.NoErr(t, err)
		assert.Eq(t, "steer:cli:hi", <-h.asked)
		assert.Eq(t, "delivered\n", stderr)
	})

	t.Run("a discarded steer exits 3", func(t *testing.T) {
		h := newSteerSendHarness(t, "session-steer-c", steered(process.ErrDiscarded))
		_, err := h.run(t, "--steer", "--wait", "hi")
		assert.Eq(t, 3, ExitCode(err))
	})

	t.Run("a steer with no running turn is queued", func(t *testing.T) {
		h := newSteerSendHarness(t, "session-steer-d", queued(nil))
		stderr, err := h.run(t, "--steer", "hi")
		assert.NoErr(t, err)
		assert.Eq(t, "steer:cli:hi", <-h.asked)
		assert.Eq(t, "未插入当前回合，已排队\n", stderr)
	})

	t.Run("a queued steer with --wait reports delivered once it runs", func(t *testing.T) {
		h := newSteerSendHarness(t, "session-steer-e", queued(nil))
		stderr, err := h.run(t, "--steer", "--wait", "hi")
		assert.NoErr(t, err)
		assert.Eq(t, "delivered\n", stderr)
	})

	t.Run("a queued steer discarded at exit exits 3", func(t *testing.T) {
		h := newSteerSendHarness(t, "session-steer-f", queued(process.ErrDiscarded))
		_, err := h.run(t, "--steer", "--wait", "hi")
		assert.Eq(t, 3, ExitCode(err))
	})
}

func TestSendSteerFullInboxReportsQueueFull(t *testing.T) {
	h := newSteerSendHarness(t, "session-steer-g", func() (process.Delivery, <-chan error, error) {
		return process.Delivery{}, nil, process.ErrQueueFull
	})
	stderr, err := h.run(t, "--steer", "hi")
	assert.Require(t, assert.NotNil(t, err))
	assert.Eq(t, 1, ExitCode(err))
	assert.StrContains(t, err.Error(), "queue_full")
	assert.Eq(t, "steer:cli:hi", <-h.asked)
	// Nothing is queued for a later turn: the sender is told instead.
	assert.StrNotContains(t, stderr, "已排队")
}

func TestSendWithoutSteerKeepsTheFollowUpMode(t *testing.T) {
	h := newSteerSendHarness(t, "session-steer-h", queued(nil))
	stderr, err := h.run(t, "hi")
	assert.NoErr(t, err)
	assert.Eq(t, "follow_up:cli:hi", <-h.asked)
	assert.Eq(t, "queued\n", stderr)
}
