package chat

import (
	"context"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
)

// Input typed before the prompt is on screen (the person types ahead while chat
// is still starting, or a resume prints the previous reply) is held by the
// terminal in cooked mode: the line discipline turns its Enter into a newline
// (ICRNL on Unix; the Windows console hands over CRLF), and the raw-mode read
// started for the prompt gets that buffered line back. It must submit like an
// Enter pressed at the prompt, otherwise the text sits in the editor and the
// turn never starts.
//
// Reproducer for the chat_edit smoke regression: the newline reached the line
// editor, which takes only CR as Enter, so the prompt hung with the text on the
// input line.

// newPretypedSource builds a terminal-mode input source whose "keyboard" already
// holds what was typed before the prompt (fakeKeyboard starts with the bytes in
// its queue, so the first read returns them at once).
func newPretypedSource(t *testing.T, typed string) *chatInputSource {
	t.Helper()
	kb := newFakeKeyboard()
	kb.Type(typed)
	source, err := newChatInputSource(context.Background(), kb, &syncOutput{})
	assert.Require(t, assert.NoErr(t, err))
	tty := &fakeTTY{}
	source.makeRaw = tty.makeRaw
	source.reader = &chatActivityReader{source: source, input: kb}
	newTestTerminal(source)
	t.Cleanup(func() { _ = source.Close(); kb.Close() })
	return source
}

// nextLine drives one prompt read the way chat's loop does (activity events are
// acknowledged, the finished line is taken) and returns the first line event. It
// fails the test instead of blocking forever when no line arrives.
func nextLine(t *testing.T, source *chatInputSource) chatInputEvent {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case event := <-source.Next():
			if event.activity {
				source.AckActivity()
				continue
			}
			source.CompleteLine()
			return event
		case <-deadline:
			t.Fatal("no line event: the line typed before the prompt was not submitted")
			return chatInputEvent{}
		}
	}
}

// assertNoLine reports that a prompt read produces no line (and no leftovers).
func assertNoLine(t *testing.T, source *chatInputSource) {
	t.Helper()
	select {
	case event := <-source.Next():
		if event.activity {
			source.AckActivity()
			assertNoLine(t, source)
			return
		}
		t.Fatalf("unexpected line event %q: input left over from the pre-typed line", event.line)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestChatInputSource_PretypedLine(t *testing.T) {
	t.Run("the newline of a pre-typed line submits it", func(t *testing.T) {
		// The line break the terminal hands over for Enter: LF on Unix (ICRNL), CRLF
		// on Windows - a bare LF is Ctrl+J there, so the test uses the platform's own.
		source := newPretypedSource(t, "把 old 改成 new"+testNewline)
		assert.True(t, source.Request("> "))
		event := nextLine(t, source)
		assert.NoErr(t, event.err)
		assert.Eq(t, "把 old 改成 new", event.line)
	})

	t.Run("a Windows console pre-type (CRLF) submits one line only", func(t *testing.T) {
		source := newPretypedSource(t, "hello\r\n")
		assert.True(t, source.Request("> "))
		// CR is the Enter; the LF right after it is the pair's second byte, not a
		// second (empty) line: the next prompt read has nothing to read.
		assert.Eq(t, "hello", nextLine(t, source).line)
		assert.True(t, source.Request("> "))
		assertNoLine(t, source)
	})
}
