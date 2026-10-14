package chat

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore/tools"
)

// fakeKeyboard models a tty input queue: typed bytes wait in a buffer until
// read, and Flush drops them (like tcflush). It also counts its Read calls, so a
// test can see whether anything read between two requests.
type fakeKeyboard struct {
	mu      sync.Mutex
	cond    *sync.Cond
	buf     []byte
	closed  bool
	flushes int
	reads   int // Read calls started
	reading int // Read calls in flight (a read waiting for input)
}

func newFakeKeyboard() *fakeKeyboard {
	k := &fakeKeyboard{}
	k.cond = sync.NewCond(&k.mu)
	return k
}

func (k *fakeKeyboard) Read(p []byte) (int, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.reads++
	k.reading++
	defer func() { k.reading-- }()
	for len(k.buf) == 0 && !k.closed {
		k.cond.Wait()
	}
	if len(k.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, k.buf)
	k.buf = k.buf[n:]
	return n, nil
}

// Reads is how many Read calls the input has seen.
func (k *fakeKeyboard) Reads() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.reads
}

// Reading is how many Read calls are in flight, that is how many reads are
// blocked waiting for input right now.
func (k *fakeKeyboard) Reading() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.reading
}

func (k *fakeKeyboard) Type(s string) {
	k.mu.Lock()
	k.buf = append(k.buf, s...)
	k.mu.Unlock()
	k.cond.Broadcast()
}

func (k *fakeKeyboard) Flush() error {
	k.mu.Lock()
	k.buf = nil
	k.flushes++
	k.mu.Unlock()
	return nil
}

func (k *fakeKeyboard) Flushes() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.flushes
}

func (k *fakeKeyboard) Unread() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.buf)
}

func (k *fakeKeyboard) Close() {
	k.mu.Lock()
	k.closed = true
	k.mu.Unlock()
	k.cond.Broadcast()
}

// syncOutput is a goroutine-safe output buffer.
type syncOutput struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (o *syncOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.Write(p)
}

func (o *syncOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

type askHarness struct {
	source *chatInputSource
	kb     *fakeKeyboard
	tty    *fakeTTY
	out    *syncOutput
}

// newAskHarness builds a terminal-mode input source on a fake keyboard whose
// flush is the injected flushInput.
func newAskHarness(t *testing.T) *askHarness {
	t.Helper()
	kb, out := newFakeKeyboard(), &syncOutput{}
	source, err := newChatInputSource(context.Background(), kb, out)
	assert.Require(t, assert.NoErr(t, err))
	tty := &fakeTTY{}
	source.makeRaw = tty.makeRaw
	source.flushInput = kb.Flush
	source.reader = &chatActivityReader{source: source, input: kb}
	newTestTerminal(source)
	t.Cleanup(func() { _ = source.Close(); kb.Close() })
	return &askHarness{source: source, kb: kb, tty: tty, out: out}
}

type askResult struct {
	line string
	err  error
}

const askPrompt = "允许执行 shell_exec？[y]是 [N]否 [a]本会话总是允许该工具："

// ask starts a question in the background, like the approver does mid-turn.
func (h *askHarness) ask() <-chan askResult {
	done := make(chan askResult, 1)
	go func() {
		line, err := h.source.Ask(context.Background(), "  命令：echo hi\n", askPrompt)
		done <- askResult{line, err}
	}()
	return done
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// waitPrompt waits until the question prompt is on screen (after the flush).
func (h *askHarness) waitPrompt(t *testing.T) {
	t.Helper()
	waitUntil(t, "question prompt", func() bool { return strings.Contains(h.out.String(), askPrompt) })
}

func (h *askHarness) answer(t *testing.T, done <-chan askResult) askResult {
	t.Helper()
	select {
	case res := <-done:
		return res
	case <-time.After(3 * time.Second):
		t.Fatal("Ask did not return")
		return askResult{}
	}
}

// nextLine reads the next prompt line event, acknowledging activity first.
func (h *askHarness) nextLine(t *testing.T) chatInputEvent {
	t.Helper()
	for {
		select {
		case event := <-h.source.Next():
			if event.activity {
				h.source.AckActivity()
				continue
			}
			h.source.CompleteLine()
			return event
		case <-time.After(3 * time.Second):
			t.Fatal("no input event")
		}
	}
}

// typeAtPrompt types text at a waiting prompt and acknowledges its activity,
// as nextChatInput does before a delivery starts a turn.
func (h *askHarness) typeAtPrompt(t *testing.T, text string) {
	t.Helper()
	h.kb.Type(text)
	select {
	case event := <-h.source.Next():
		assert.True(t, event.activity)
		h.source.AckActivity()
	case <-time.After(3 * time.Second):
		t.Fatal("no activity event")
	}
	waitUntil(t, "typed text echoed", func() bool { return strings.Contains(h.out.String(), text) })
}

func TestChatInputSource_AskAnswers(t *testing.T) {
	for _, tc := range []struct{ name, typed, want string }{
		{"yes", "y\r", "y"},
		{"no", "n\r", "n"},
		{"always", "a\r", "a"},
		{"empty line", "\r", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newAskHarness(t)
			h.source.SetBusy(true) // a turn is running; no read is pending
			done := h.ask()
			h.waitPrompt(t)
			assert.True(t, h.tty.isRaw()) // the question is read in raw mode
			h.kb.Type(tc.typed)
			res := h.answer(t, done)
			assert.NoErr(t, res.err)
			assert.Eq(t, tc.want, res.line)
			assert.Eq(t, 1, h.kb.Flushes())
			// Back to the turn state: cooked, turnActive.
			waitTTY(t, h.tty, false)
			h.source.mu.Lock()
			assert.True(t, h.source.turnActive)
			assert.False(t, h.source.pending)
			h.source.mu.Unlock()
			assert.StrContains(t, h.out.String(), "命令：echo hi")
		})
	}

	for _, tc := range []struct{ name, key string }{{"ctrl-c", "\x03"}, {"ctrl-d", "\x04"}} {
		key := tc.key
		t.Run(tc.name+" cancels and the next prompt still works", func(t *testing.T) {
			h := newAskHarness(t)
			h.source.SetBusy(true)
			done := h.ask()
			h.waitPrompt(t)
			h.kb.Type(key)
			res := h.answer(t, done)
			assert.ErrIs(t, res.err, errAskCancelled)
			assert.ErrIs(t, res.err, context.Canceled)
			assert.False(t, errors.Is(res.err, tools.ErrDenied))
			waitTTY(t, h.tty, false)

			h.source.SetBusy(false)
			assert.True(t, h.source.Request("> "))
			h.kb.Type("ok\r")
			event := h.nextLine(t)
			assert.NoErr(t, event.err)
			assert.Eq(t, "ok", event.line)
		})
	}
}

// TestChatInputSource_AskIgnoresEarlyInput: nothing typed before the question
// can approve it.
func TestChatInputSource_AskIgnoresEarlyInput(t *testing.T) {
	t.Run("unread typed input is flushed", func(t *testing.T) {
		h := newAskHarness(t)
		h.source.SetBusy(true)
		h.kb.Type("y\r") // typed during the turn, nobody reading yet
		done := h.ask()
		h.waitPrompt(t)
		assert.Eq(t, 0, h.kb.Unread())
		h.kb.Type("n\r")
		res := h.answer(t, done)
		assert.NoErr(t, res.err)
		assert.Eq(t, "n", res.line)
	})

	t.Run("byte held in the activity handshake is dropped", func(t *testing.T) {
		h := newAskHarness(t)
		assert.True(t, h.source.Request("> "))
		waitTTY(t, h.tty, true)
		h.source.SetBusy(true) // a delivery started the turn while the prompt waited
		h.kb.Type("y\r")
		// The reader holds 'y' until its activity event is acknowledged.
		waitUntil(t, "activity event", func() bool { return len(h.source.events) == 1 })
		done := h.ask()
		h.waitPrompt(t)
		h.kb.Type("\r") // just Enter: must not become "y"
		res := h.answer(t, done)
		assert.NoErr(t, res.err)
		assert.Eq(t, "", res.line)
	})

	t.Run("line finished during the turn is the next input, not the answer", func(t *testing.T) {
		h := newAskHarness(t)
		assert.True(t, h.source.Request("> "))
		waitTTY(t, h.tty, true)
		h.typeAtPrompt(t, "y")
		h.source.SetBusy(true) // a delivery started the turn
		h.kb.Type("\r")        // the pending read finishes "y" during the turn
		waitUntil(t, "finished line", func() bool { return len(h.source.events) == 1 })

		done := h.ask()
		h.waitPrompt(t)
		h.kb.Type("n\r")
		res := h.answer(t, done)
		assert.NoErr(t, res.err)
		assert.Eq(t, "n", res.line)

		// After the turn the kept "y" is delivered as the next prompt input.
		h.source.SetBusy(false)
		assert.True(t, h.source.Request("> "))
		event := h.nextLine(t)
		assert.NoErr(t, event.err)
		assert.Eq(t, "y", event.line)
	})
}

func TestChatInputSource_AskTakesHalfLine(t *testing.T) {
	h := newAskHarness(t)
	assert.True(t, h.source.Request("> "))
	waitTTY(t, h.tty, true)
	h.typeAtPrompt(t, "hel")
	h.source.SetBusy(true) // a delivery started the turn while "hel" was being typed

	done := h.ask()
	h.waitPrompt(t)
	h.kb.Type("y\r")
	res := h.answer(t, done)
	assert.NoErr(t, res.err)
	assert.Eq(t, "y", res.line) // not "hely"

	// The next prompt read starts with the half line and the original prompt.
	h.source.SetBusy(false)
	before := len(h.out.String())
	assert.True(t, h.source.Request("> "))
	waitUntil(t, "prefilled prompt", func() bool { return strings.Contains(h.out.String()[before:], "> hel") })
	h.kb.Type("lo\r")
	event := h.nextLine(t)
	assert.NoErr(t, event.err)
	assert.Eq(t, "hello", event.line)

	// History has "hello" but not the answer "y": Up twice stays on "hello".
	assert.True(t, h.source.Request("> "))
	h.kb.Type("\x1b[A\x1b[A\r")
	event = h.nextLine(t)
	assert.Eq(t, "hello", event.line)
}

func TestChatInputSource_AskKeepsDeliveriesQueued(t *testing.T) {
	h := newAskHarness(t)
	h.source.SetBusy(true)
	done := h.ask()
	h.waitPrompt(t)
	queued, settled, err := h.source.DeliverWait("from elsewhere", "cli")
	assert.NoErr(t, err)
	assert.True(t, queued)
	h.kb.Type("y\r")
	res := h.answer(t, done)
	assert.NoErr(t, res.err)
	assert.Eq(t, "y", res.line)
	// The delivery is still queued for the chat loop and not settled yet.
	assert.Eq(t, 1, len(h.source.deliveries))
	select {
	case <-settled:
		t.Fatal("delivery settled during the question")
	default:
	}
}

func TestChatInputSource_AskTwiceInOneTurn(t *testing.T) {
	h := newAskHarness(t)
	assert.True(t, h.source.Request("> "))
	waitTTY(t, h.tty, true)
	h.typeAtPrompt(t, "ab")
	h.source.SetBusy(true)
	for _, typed := range []string{"y\r", "n\r"} {
		before := h.kb.Flushes()
		done := h.ask()
		waitUntil(t, "flush", func() bool { return h.kb.Flushes() == before+1 })
		h.waitPrompt(t)
		h.kb.Type(typed)
		res := h.answer(t, done)
		assert.NoErr(t, res.err)
		assert.Eq(t, strings.TrimSuffix(typed, "\r"), res.line)
	}
	h.source.SetBusy(false)
	assert.True(t, h.source.Request("> "))
	h.kb.Type("c\r")
	assert.Eq(t, "abc", h.nextLine(t).line)
}

func TestChatInputSource_AskWithoutTerminal(t *testing.T) {
	source, err := newChatInputSource(context.Background(), strings.NewReader("y\n"), io.Discard)
	assert.Require(t, assert.NoErr(t, err))
	t.Cleanup(func() { _ = source.Close() })
	_, err = source.Ask(context.Background(), "", askPrompt)
	assert.ErrIs(t, err, tools.ErrCannotAsk)
}
