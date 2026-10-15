package chat

import (
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// TestAltEnterInsertsNewline: Alt+Enter (ESC then Enter) asks for a newline inside
// the input instead of submitting it, which is what makes a multi-line input
// reachable. It is the only unambiguous key for that: a bare LF (Ctrl+J) is also how
// a pre-typed line ends on Unix (ICRNL). An escape sequence that is not Alt+Enter
// still reaches the editor whole.
func TestAltEnterInsertsNewline(t *testing.T) {
	newSource := func(t *testing.T) *askHarness {
		t.Helper()
		h := newAskHarness(t)
		h.source.inputWait = kbWait(h.kb)
		assert.True(t, h.source.Request("> "))
		return h
	}

	t.Run("ESC then CR inserts a newline and the input goes on", func(t *testing.T) {
		h := newSource(t)
		h.kb.Type("第一行\x1b\r第二行") // Alt+Enter between the two lines
		assertNoLine(t, h.source)
		h.kb.Type("\r") // the person's own Enter submits
		event := h.nextLine(t)
		assert.NoErr(t, event.err)
		assert.Eq(t, "第一行\n第二行", event.line)
	})

	t.Run("an arrow key still reaches the editor as a sequence", func(t *testing.T) {
		h := newSource(t)
		h.kb.Type("ab\x1b[Dc\r") // Left arrow, then c: the cursor moved first
		event := h.nextLine(t)
		assert.NoErr(t, event.err)
		assert.Eq(t, "acb", event.line)
	})
}
