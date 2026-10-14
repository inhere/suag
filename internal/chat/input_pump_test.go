package chat

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
)

// kbWaiter stands in for the platform wait behind the decisions of the byte pump
// (waitTerminalInput): it answers "is a byte ready?" from the fake keyboard's
// buffer, exactly as the real wait answers from the terminal's input, and records
// the timeouts it was asked for — the lone-Esc wait and the pasted-newline wait
// share it.
type kbWaiter struct {
	kb       *fakeKeyboard
	mu       sync.Mutex
	timeouts []time.Duration
}

func newKBWaiter(kb *fakeKeyboard) *kbWaiter { return &kbWaiter{kb: kb} }

func (w *kbWaiter) wait(timeout time.Duration) bool {
	w.mu.Lock()
	w.timeouts = append(w.timeouts, timeout)
	w.mu.Unlock()
	return w.kb.Unread() > 0
}

func (w *kbWaiter) calls() []time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]time.Duration(nil), w.timeouts...)
}

// escWaits is the lone-Esc waits among the recorded ones: the same injected wait
// also answers the pasted-newline decision (pasteNewlineTimeout).
func escWaits(w *kbWaiter) []time.Duration {
	var only []time.Duration
	for _, d := range w.calls() {
		if d == escWaitTimeout {
			only = append(only, d)
		}
	}
	return only
}

// requestSteer asks for a steer read (the prompt steering.go draws) and asserts
// it was accepted.
func (h *askHarness) requestSteer(t *testing.T) {
	t.Helper()
	_, ok := h.source.RequestSteer(steerPrompt)
	assert.Require(t, assert.True(t, ok))
}

// nextTurnEvent reads the next event of a steer read.
func (h *askHarness) nextTurnEvent(t *testing.T) chatInputEvent {
	t.Helper()
	select {
	case event := <-h.source.TurnEvents():
		return event
	case <-time.After(3 * time.Second):
		t.Fatal("no turn event")
		return chatInputEvent{}
	}
}

// TestChatInputSource_SteerRead: how a read during a turn interprets input
// (design D2/N4). No chat path turns this on yet (S2b does); the input line is
// already read this way.
func TestChatInputSource_SteerRead(t *testing.T) {
	t.Run("Enter submits an inserted message and sends no activity", func(t *testing.T) {
		h := newAskHarness(t)
		h.source.SetBusy(true)
		h.requestSteer(t)
		h.kb.Type("只改 README\r")
		event := h.nextTurnEvent(t)
		assert.Eq(t, "只改 README", event.line)
		assert.Eq(t, keyNone, event.key)
		// A turn has no waiting prompt to interrupt: a steer read sends no activity
		// event, and the message never reaches another mode's channel.
		assert.Eq(t, 0, len(h.source.events))
		assert.Eq(t, 0, len(h.source.answers))
	})

	t.Run("Ctrl-C ends the turn, a second one within the window leaves chat", func(t *testing.T) {
		h := newAskHarness(t)
		h.source.SetBusy(true)
		h.requestSteer(t)
		h.kb.Type("\x03")
		assert.Eq(t, keyAbort, h.nextTurnEvent(t).key)
		// The clock is the input source's, so this press is the second of the two.
		h.kb.Type("\x03")
		assert.Eq(t, keyExit, h.nextTurnEvent(t).key)
	})

	t.Run("the interrupt clock is shared with the prompt read", func(t *testing.T) {
		h := newAskHarness(t)
		h.source.SetBusy(true)
		h.requestSteer(t)
		h.kb.Type("\x03")
		assert.Eq(t, keyAbort, h.nextTurnEvent(t).key)
		// The turn is over and chat is back at the prompt: the Ctrl-C that ended it
		// counts as the first of the two, so the prompt read asking the same source
		// (chat.go) would see the next one as the confirmation.
		assert.True(t, h.source.takeInterrupt())
		// Anything else the person does starts the confirmation over.
		h.source.clearInterrupt()
		assert.False(t, h.source.takeInterrupt())
	})

	t.Run("Ctrl-D during a turn is ignored", func(t *testing.T) {
		h := newAskHarness(t)
		h.source.SetBusy(true)
		h.requestSteer(t)
		h.kb.Type("\x04")
		// The read goes on for the turn, so the next line is the inserted message.
		h.kb.Type("还在吗\r")
		event := h.nextTurnEvent(t)
		assert.Eq(t, "还在吗", event.line)
		assert.Eq(t, keyNone, event.key)
	})
}

// TestChatInputSource_EscKey: a lone Esc is a key only where the wait for a
// follow-up byte says so (a steer read); every other read keeps x/term's
// behaviour, the Esc byte waiting for the rest of a sequence.
func TestChatInputSource_EscKey(t *testing.T) {
	t.Run("a lone Esc asks to end the turn after the 50ms wait", func(t *testing.T) {
		h := newAskHarness(t)
		waiter := newKBWaiter(h.kb) // nothing follows the 0x1b
		h.source.inputWait = waiter.wait
		h.source.SetBusy(true)
		h.requestSteer(t)
		h.kb.Type("\x1b")
		assert.Eq(t, keyAbort, h.nextTurnEvent(t).key)
		assert.Eq(t, []time.Duration{escWaitTimeout}, escWaits(waiter))
		assert.Eq(t, 50*time.Millisecond, escWaitTimeout)
	})

	t.Run("an escape sequence stays a sequence", func(t *testing.T) {
		h := newAskHarness(t)
		waiter := newKBWaiter(h.kb) // the rest of the sequence is already there
		h.source.inputWait = waiter.wait
		h.source.SetBusy(true)
		h.requestSteer(t)
		h.kb.Type("ab\x1b[Dc\r")
		event := h.nextTurnEvent(t)
		// The Left key moved the cursor, so the 'c' went in before the 'b': the
		// Esc byte was handled as the sequence it is, not as the Esc key.
		assert.Eq(t, "acb", event.line)
		assert.Eq(t, keyNone, event.key)
		assert.True(t, len(escWaits(waiter)) > 0)
	})

	t.Run("a prompt read leaves the Esc to the editor as a sequence", func(t *testing.T) {
		h := newAskHarness(t)
		waiter := newKBWaiter(h.kb) // would report a lone Esc
		h.source.inputWait = waiter.wait
		assert.True(t, h.source.Request("> "))
		h.kb.Type("\x1b")
		time.Sleep(20 * time.Millisecond)      // give a wrong decision time to happen
		assert.Eq(t, 0, len(escWaits(waiter))) // the mode decides: no wait, no Esc key
		assert.Eq(t, 0, len(h.source.turnEvents))
		// The Esc and the letter after it are one unknown key sequence, as with
		// x/term: the submitted line is empty.
		h.kb.Type("x\r")
		assert.Eq(t, "", h.nextLine(t).line)
	})
}

// TestChatInputSource_ReadsOnlyOnRequest: the read contract (design F2) — input is
// read only while a request is outstanding, so what is typed between two requests
// is not taken early.
func TestChatInputSource_ReadsOnlyOnRequest(t *testing.T) {
	h := newAskHarness(t)
	// No request yet: the keyboard is not read at all and the bytes wait in it.
	h.kb.Type("early\r")
	time.Sleep(20 * time.Millisecond)
	assert.Eq(t, 0, h.kb.Reads())
	assert.Eq(t, 0, h.kb.Reading())
	assert.Eq(t, len("early\r"), h.kb.Unread())

	// The request reads what was typed while nobody asked.
	assert.True(t, h.source.Request("> "))
	assert.Eq(t, "early", h.nextLine(t).line)
	h.source.CompleteLine()

	// Between the requests nothing reads, so the next line waits in the terminal
	// until the next request asks for it.
	reads := h.kb.Reads()
	h.kb.Type("later\r")
	time.Sleep(20 * time.Millisecond)
	assert.Eq(t, reads, h.kb.Reads())
	assert.Eq(t, 0, h.kb.Reading())
	assert.Eq(t, len("later\r"), h.kb.Unread())

	assert.True(t, h.source.Request("> "))
	assert.Eq(t, "later", h.nextLine(t).line)
}

// TestChatInputSource_StickyEOF: once the input has ended, every later request
// answers EOF at once instead of blocking in a read that will not return.
func TestChatInputSource_StickyEOF(t *testing.T) {
	kb := newFakeKeyboard()
	kb.Close() // an input that is done: Read answers io.EOF
	source, err := newChatInputSource(context.Background(), kb, io.Discard)
	assert.Require(t, assert.NoErr(t, err))
	t.Cleanup(func() { _ = source.Close(); kb.Close() })

	assert.True(t, source.Request("> "))
	assert.True(t, errors.Is((<-source.Next()).err, io.EOF))
	source.CompleteLine()

	reads := kb.Reads()
	assert.True(t, source.Request("> "))
	select {
	case event := <-source.Next():
		assert.True(t, errors.Is(event.err, io.EOF))
	case <-time.After(time.Second):
		t.Fatal("the second request did not answer EOF")
	}
	// The sticky EOF answered without reading the input again.
	assert.Eq(t, reads, kb.Reads())
	assert.Eq(t, 0, kb.Reading())
}

// TestChatInputSource_AskInputStaysOffEvents: during an approval question the input
// is read in ask mode, so neither the chat loop's channel nor the turn channel
// sees a byte of the answer (design N5).
func TestChatInputSource_AskInputStaysOffEvents(t *testing.T) {
	h := newAskHarness(t)
	h.source.SetBusy(true)
	done := h.ask()
	h.waitPrompt(t)
	h.kb.Type("y\r")
	res := h.answer(t, done)
	assert.NoErr(t, res.err)
	assert.Eq(t, "y", res.line)
	assert.Eq(t, 0, len(h.source.events))
	assert.Eq(t, 0, len(h.source.turnEvents))
}
