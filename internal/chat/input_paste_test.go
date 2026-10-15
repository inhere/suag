package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
)

// kbWait answers the byte-ready wait (waitTerminalInput on a real terminal) from
// the fake keyboard's buffer (kbWaiter, input_pump_test.go): a byte is ready while
// the keyboard still holds input. It stands in for the platform wait behind the
// pasted-newline decision.
func kbWait(kb *fakeKeyboard) func(time.Duration) bool {
	return newKBWaiter(kb).wait
}

// TestChatInputSource_PastedNewlines: a multi-line paste reaches chat as one
// input on the input line, and only the person's own Enter submits it — as one
// message whose internal newlines are kept. Two ways to tell a pasted newline
// from a typed Enter are covered: the terminal brackets the paste (the markers
// put the editor in paste mode) and it does not, where the pump goes by the byte
// that follows the newline within pasteNewlineTimeout (the injected wait).
func TestChatInputSource_PastedNewlines(t *testing.T) {
	// newSource is a terminal-mode source whose byte-ready wait reads the fake
	// keyboard, so the pump's newline decision sees the queued bytes of a paste.
	newSource := func(t *testing.T) *askHarness {
		t.Helper()
		h := newAskHarness(t)
		h.source.inputWait = kbWait(h.kb)
		assert.True(t, h.source.Request("> "))
		return h
	}

	t.Run("a bracketed paste stays on the line until the person's Enter", func(t *testing.T) {
		h := newSource(t)
		h.kb.Type("\x1b[200~第一行\n第二行\x1b[201~")
		// The paste does not submit anything: the whole text is on the input line.
		assertNoLine(t, h.source)
		h.kb.Type("\r")
		event := h.nextLine(t)
		assert.NoErr(t, event.err)
		assert.Eq(t, "第一行\n第二行", event.line)
	})

	t.Run("without markers the newline of a paste is told by the byte after it", func(t *testing.T) {
		h := newSource(t)
		h.kb.Type("第一行" + testNewline + "第二行" + testNewline) // the paste arrives as one chunk
		event := h.nextLine(t)
		assert.NoErr(t, event.err)
		assert.Eq(t, "第一行\n第二行", event.line)
		// One event for the whole paste: the line break inside it was text.
		assertNoLine(t, h.source)
	})

	t.Run("a CR line break of a paste is text as well", func(t *testing.T) {
		h := newSource(t)
		h.kb.Type("a\rb\r")
		event := h.nextLine(t)
		assert.NoErr(t, event.err)
		assert.Eq(t, "a\nb", event.line)
		assertNoLine(t, h.source)
	})

	t.Run("a typed Enter still submits", func(t *testing.T) {
		h := newSource(t)
		h.kb.Type("hi\r") // nothing follows the CR: Enter, not a pasted newline
		event := h.nextLine(t)
		assert.NoErr(t, event.err)
		assert.Eq(t, "hi", event.line)
		assertNoLine(t, h.source)
	})

	t.Run("an LF that follows the CR of a typed Enter is the pair's second byte", func(t *testing.T) {
		// The Windows console hands a typed Enter over as CRLF: the LF belongs to
		// the CR, so it neither submits an empty line nor makes the CR pasted text.
		h := newSource(t)
		h.kb.Type("hello\r\n")
		assert.Eq(t, "hello", h.nextLine(t).line)
		assert.True(t, h.source.Request("> "))
		assertNoLine(t, h.source)
	})
}

// TestChatInputSource_BracketedPasteMode: chat asks the terminal to bracket
// pastes (ESC[?2004h) for as long as a line is being edited and turns it off
// again (ESC[?2004l) as soon as the line is done, so a cooked-mode read never
// meets the markers themselves.
func TestChatInputSource_BracketedPasteMode(t *testing.T) {
	const on, off = bracketPasteOn, bracketPasteOff

	t.Run("on while the line is edited, off once it is submitted", func(t *testing.T) {
		h := newAskHarness(t)
		assert.True(t, h.source.Request("> "))
		waitUntil(t, "bracketed paste on", func() bool { return strings.Contains(h.out.String(), on) })
		h.kb.Type("hi\r")
		assert.Eq(t, "hi", h.nextLine(t).line)
		waitUntil(t, "bracketed paste off", func() bool { return strings.Contains(h.out.String(), off) })
	})

	t.Run("off again when a turn keeps the terminal cooked", func(t *testing.T) {
		h := newAskHarness(t)
		assert.True(t, h.source.Request("> "))
		waitUntil(t, "bracketed paste on", func() bool { return strings.Contains(h.out.String(), on) })
		before := len(h.out.String())
		h.source.SetBusy(true) // the turn puts the terminal back to cooked mode
		waitUntil(t, "bracketed paste off", func() bool { return strings.Contains(h.out.String()[before:], off) })
	})

	t.Run("an answer read brackets pastes too", func(t *testing.T) {
		h := newAskHarness(t)
		h.source.SetBusy(true)
		done := h.ask()
		h.waitPrompt(t)
		// The question made the read raw again: the mode is on before the answer.
		waitUntil(t, "bracketed paste on", func() bool { return strings.Contains(h.out.String(), on) })
		h.kb.Type("y\r")
		assert.Eq(t, "y", h.answer(t, done).line)
	})
}
