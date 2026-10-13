package chat

import (
	"context"
	"errors"
	"github.com/inhere/suga/internal/personal/process"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/chat/lineedit"
)

// fakeTTY records raw/cooked transitions without touching a real terminal.
type fakeTTY struct {
	mu       sync.Mutex
	raw      bool
	makes    int
	restores int
}

func (f *fakeTTY) makeRaw() (func() error, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.raw = true
	f.makes++
	return func() error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.raw = false
		f.restores++
		return nil
	}, nil
}

func (f *fakeTTY) isRaw() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.raw
}

func waitTTY(t *testing.T, f *fakeTTY, raw bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if f.isRaw() == raw {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("tty raw=%v never reached (now %v)", raw, f.isRaw())
}

// newFakeTTYSource builds a terminal-mode input source whose "keyboard" is the returned pipe.
func newFakeTTYSource(t *testing.T) (*chatInputSource, *fakeTTY, *io.PipeWriter) {
	t.Helper()
	pr, pw := io.Pipe()
	source, err := newChatInputSource(context.Background(), pr, io.Discard)
	assert.Require(t, assert.NoErr(t, err))
	tty := &fakeTTY{}
	source.makeRaw = tty.makeRaw
	source.reader = &chatActivityReader{source: source, input: pr}
	source.terminal = lineedit.NewTerminal(&chatTerminalIO{reader: source.reader, output: source.output}, "")
	t.Cleanup(func() { _ = source.Close(); _ = pw.Close(); _ = pr.Close() })
	return source, tty, pw
}

func TestChatInputSource_RawModeFollowsTurns(t *testing.T) {
	t.Run("turn started by delivery while prompt waits restores cooked", func(t *testing.T) {
		source, tty, _ := newFakeTTYSource(t)
		assert.True(t, source.Request("> "))
		waitTTY(t, tty, true) // reading the prompt line: raw

		source.SetBusy(true) // a delivery starts a turn while the read is still blocked
		assert.False(t, tty.isRaw())

		source.SetBusy(false)
		assert.False(t, tty.isRaw()) // between turn end and the next prompt stays cooked

		assert.True(t, source.Request("> ")) // pending read is reused for the new prompt
		assert.True(t, tty.isRaw())
	})

	t.Run("read goroutine starting after the turn began stays cooked", func(t *testing.T) {
		source, tty, _ := newFakeTTYSource(t)
		source.SetBusy(true)
		source.mu.Lock()
		err := source.enterRawLocked() // what readLine would do if it lost the race
		source.mu.Unlock()
		assert.NoErr(t, err)
		assert.False(t, tty.isRaw())
		assert.Eq(t, 0, tty.makes)
	})

	t.Run("normal prompt cycle enters and leaves raw once per line", func(t *testing.T) {
		source, tty, pw := newFakeTTYSource(t)
		assert.True(t, source.Request("> "))
		waitTTY(t, tty, true)
		go func() { _, _ = io.WriteString(pw, "hi\r") }()
		event := <-source.Next()
		assert.True(t, event.activity)
		source.AckActivity()
		event = <-source.Next()
		assert.NoErr(t, event.err)
		assert.Eq(t, "hi", event.line)
		source.CompleteLine()
		waitTTY(t, tty, false)
	})

	t.Run("close leaves cooked and refuses raw", func(t *testing.T) {
		source, tty, _ := newFakeTTYSource(t)
		assert.True(t, source.Request("> "))
		waitTTY(t, tty, true)
		assert.NoErr(t, source.Close())
		assert.False(t, tty.isRaw())
		assert.NoErr(t, source.enterRaw())
		assert.False(t, tty.isRaw())
	})
}

func TestChatInputSource_DiscardQueued(t *testing.T) {
	source, err := newChatInputSource(context.Background(), strings.NewReader(""), io.Discard)
	assert.Require(t, assert.NoErr(t, err))
	t.Cleanup(func() { _ = source.Close() })
	_, first, err := source.DeliverWait("one", "cli")
	assert.NoErr(t, err)
	_, second, err := source.DeliverWait("two", "cli")
	assert.NoErr(t, err)

	assert.Eq(t, 2, source.DiscardQueued())
	assert.True(t, errors.Is(<-first, process.ErrDiscarded))
	assert.True(t, errors.Is(<-second, process.ErrDiscarded))
	assert.Eq(t, 0, source.DiscardQueued())

	_, _, err = source.DeliverWait("late", "cli")
	assert.True(t, errors.Is(err, process.ErrSessionClosed))
}

func TestDeliveryLine(t *testing.T) {
	for _, tc := range []struct {
		name, text, from, want string
	}{
		{name: "send default source", text: "hello", from: "cli", want: "[来自 cli] hello"},
		{name: "send --source", text: "hello", from: "web-ui", want: "[来自 web-ui] hello"},
		{name: "gofer is not special", text: "hello", from: "gofer", want: "[来自 gofer] hello"},
		{name: "empty source", text: "hello", from: " ", want: "[来自 unknown] hello"},
		{name: "control chars dropped", text: "hello", from: "a\x1bb\n", want: "[来自 ab] hello"},
		{name: "existing tag kept", text: "[x] hello", from: "gofer", want: "[x] hello"},
		{name: "sender's own reply tag kept", text: "[gofer web 回复] hi", from: "gofer", want: "[gofer web 回复] hi"},
		{name: "existing source tag kept", text: "[来自 bot] hi", from: "cli", want: "[来自 bot] hi"},
		{name: "tag needs a following space", text: "[x]hello", from: "cli", want: "[来自 cli] [x]hello"},
		{name: "tag must close on the first line", text: "[x\n] hello", from: "cli", want: "[来自 cli] [x\n] hello"},
		{name: "empty brackets are not a tag", text: "[] hello", from: "cli", want: "[来自 cli] [] hello"},
		{name: "tag must lead the text", text: " [x] hello", from: "cli", want: "[来自 cli]  [x] hello"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Eq(t, tc.want, deliveryLine(tc.text, tc.from))
		})
	}
}
