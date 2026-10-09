package lineedit

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
)

// historyLen counts the history entries.
func historyLen(tm *Terminal) int {
	tm.lock.Lock()
	defer tm.lock.Unlock()
	n := 0
	for {
		if _, ok := tm.history.NthPreviousEntry(n); !ok {
			return n
		}
		n++
	}
}

func waitOutput(t *testing.T, out *syncBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(out.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("output never contained %q: %q", want, out.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTerminal_TakeLine(t *testing.T) {
	t.Run("takes the line of a blocked read and keeps the answer out of history", func(t *testing.T) {
		pr, pw := io.Pipe()
		out := &syncBuffer{}
		tm := NewTerminal(struct {
			io.Reader
			io.Writer
		}{pr, out}, "> ")
		lines := make(chan string, 1)
		go func() {
			line, _ := tm.ReadLine()
			lines <- line
		}()
		_, _ = pw.Write([]byte("ab"))
		waitOutput(t, out, "> ab")

		before := len(out.String())
		line, reading := tm.TakeLine() // the reader is blocked in Read: no deadlock
		assert.True(t, reading)
		assert.Eq(t, "ab", line)
		assert.StrContains(t, out.String()[before:], "  ") // the two cells are blanked

		_, _ = pw.Write([]byte("y\r"))
		assert.Eq(t, "y", <-lines) // only what was typed after TakeLine
		assert.Eq(t, 0, historyLen(tm))
		_ = pw.Close()
	})

	t.Run("without a read takes the pending line", func(t *testing.T) {
		tm, _ := newMem("> ", 0)
		line, reading := tm.TakeLine()
		assert.False(t, reading)
		assert.Eq(t, "", line)

		tm.SetPendingLine("half")
		line, reading = tm.TakeLine()
		assert.False(t, reading)
		assert.Eq(t, "half", line)
		line, _ = tm.TakeLine()
		assert.Eq(t, "", line) // taken once
	})
}

func TestTerminal_SetPendingLine(t *testing.T) {
	t.Run("prefills the next read", func(t *testing.T) {
		tm, mem := newMem("> ", 0)
		tm.SetPendingLine("ab")
		lines := readAll(t, tm, mem, "c\r")
		assert.Eq(t, []string{"abc"}, lines)
		assert.True(t, strings.HasPrefix(mem.out.String(), "> abc"))
		assert.Eq(t, 1, historyLen(tm)) // a normal read: in history
	})

	t.Run("is used once", func(t *testing.T) {
		tm, mem := newMem("> ", 0)
		tm.SetPendingLine("ab")
		lines := readAll(t, tm, mem, "\rx\r")
		assert.Eq(t, []string{"ab", "x"}, lines)
	})

	t.Run("repaints when the prompt is already on screen", func(t *testing.T) {
		tm, mem := newMem("> ", 0)
		// Ctrl-C ends a read mid-line with the prompt still on screen.
		mem.in = append(mem.in, "zz\x03")
		_, err := tm.ReadLine()
		assert.ErrIs(t, err, io.EOF)
		tm.lock.Lock()
		tm.line, tm.pos = nil, 0 // as if the line had been taken
		tm.lock.Unlock()
		tm.SetPendingLine("ab")
		before := mem.out.Len()
		mem.in = append(mem.in, "\r")
		line, err := tm.ReadLine()
		assert.NoErr(t, err)
		assert.Eq(t, "ab", line)
		repaint := mem.out.String()[before:]
		assert.StrContains(t, repaint, "\x1b[K")
		assert.StrContains(t, repaint, "> ab")
	})
}

func TestTerminal_ReadLineNoHistory(t *testing.T) {
	tm, mem := newMem("> ", 0)
	mem.in = append(mem.in, "y\r")
	line, err := tm.ReadLineNoHistory()
	assert.NoErr(t, err)
	assert.Eq(t, "y", line)
	assert.Eq(t, 0, historyLen(tm))

	// The next normal read is recorded again.
	assert.Eq(t, []string{"hello"}, readAll(t, tm, mem, "hello\r"))
	assert.Eq(t, 1, historyLen(tm))
}

func TestTerminal_AbortLine(t *testing.T) {
	t.Run("ctrl-c is consumed and the next read starts clean", func(t *testing.T) {
		tm, mem := newMem("? ", 0)
		mem.in = append(mem.in, "n\x03")
		_, err := tm.ReadLine()
		assert.ErrIs(t, err, io.EOF)
		tm.AbortLine()
		assert.True(t, strings.HasSuffix(mem.out.String(), "? n\r\n"))

		tm.SetPrompt("> ")
		before := mem.out.Len()
		mem.in = append(mem.in, "ok\r")
		line, err := tm.ReadLine() // no stale Ctrl-C, no leftover "n"
		assert.NoErr(t, err)
		assert.Eq(t, "ok", line)
		assert.Eq(t, "> ok\r\n", mem.out.String()[before:])
	})

	t.Run("ctrl-d on an empty line", func(t *testing.T) {
		tm, mem := newMem("? ", 0)
		mem.in = append(mem.in, "\x04x\r")
		_, err := tm.ReadLine()
		assert.ErrIs(t, err, io.EOF)
		tm.AbortLine()
		line, err := tm.ReadLine()
		assert.NoErr(t, err)
		assert.Eq(t, "x", line)
	})
}
