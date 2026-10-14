package chat

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal"
	"github.com/inhere/suga/pkg/agentcore/mock"
)

// TestTitleFor: the title names the program, the session's short name and the
// session title; a part that is empty (or empty after cleaning) is dropped.
func TestTitleFor(t *testing.T) {
	t.Run("name and title", func(t *testing.T) {
		assert.Eq(t, "suga · ws-k7 · 帮我改 chat 的标题", titleFor("ws-k7", "帮我改 chat 的标题"))
	})
	t.Run("no session title yet", func(t *testing.T) {
		assert.Eq(t, "suga · ws-k7", titleFor("ws-k7", ""))
	})
	t.Run("no short name", func(t *testing.T) {
		assert.Eq(t, "suga · 你好", titleFor("", "你好"))
	})
	t.Run("program only", func(t *testing.T) {
		assert.Eq(t, "suga", titleFor("", ""))
	})
	t.Run("blank parts are dropped", func(t *testing.T) {
		assert.Eq(t, "suga · 你好", titleFor("   ", " 你好 "))
	})
}

// TestCleanTitlePart: what may reach the terminal inside the OSC 0 sequence.
// A control character would end the sequence early and let the rest of the title
// through as terminal commands, so none survives.
func TestCleanTitlePart(t *testing.T) {
	t.Run("plain text is kept", func(t *testing.T) {
		assert.Eq(t, "帮我改 bug", cleanTitlePart("帮我改 bug"))
	})
	t.Run("ESC, BEL and C1 controls are dropped", func(t *testing.T) {
		// An injected title: \x1b]2;pwned\x07 must not end the sequence early,
		// and neither may the C1 form of it (U+009B, "CSI" as one rune).
		assert.Eq(t, "suga]2;pwned", cleanTitlePart("suga\x1b]2;pwned\x07"))
		assert.Eq(t, "abc", cleanTitlePart("a\u009bb\u009dc"))
	})
	t.Run("a raw C1 byte cannot end the sequence either", func(t *testing.T) {
		// Not valid UTF-8: it reads back as the replacement character, which no
		// terminal takes for a control character.
		assert.Eq(t, "a�bc", cleanTitlePart("a\x9bb\x07c"))
	})
	t.Run("DEL is dropped", func(t *testing.T) {
		assert.Eq(t, "ab", cleanTitlePart("a\x7fb"))
	})
	t.Run("newline and tab become one space", func(t *testing.T) {
		assert.Eq(t, "a b", cleanTitlePart("a\n\tb"))
		assert.Eq(t, "a b", cleanTitlePart("a\r\nb"))
	})
	t.Run("leading and trailing space is trimmed", func(t *testing.T) {
		assert.Eq(t, "想法", cleanTitlePart("\n 想法 \n"))
	})
	t.Run("capped at 30 runes", func(t *testing.T) {
		assert.Eq(t, 30, len([]rune(cleanTitlePart(strings.Repeat("长", 40)))))
		assert.Eq(t, strings.Repeat("长", 30), cleanTitlePart(strings.Repeat("长", 40)))
	})
	t.Run("a title at the cap is untouched", func(t *testing.T) {
		title := strings.Repeat("a", 30)
		assert.Eq(t, title, cleanTitlePart(title))
	})
}

// TestTerminalTitleSet: the terminal's own title is saved once, before the first
// title chat writes; the title itself is OSC 0.
func TestTerminalTitleSet(t *testing.T) {
	t.Run("save then set", func(t *testing.T) {
		var buf bytes.Buffer
		title := newTerminalTitle(&buf, &buf, true)
		title.Set("ws-k7", "你好")
		assert.Eq(t, titleSaveSequence+"\x1b]0;suga · ws-k7 · 你好\x07", buf.String())
	})
	t.Run("the saved title is pushed once", func(t *testing.T) {
		var buf bytes.Buffer
		title := newTerminalTitle(&buf, &buf, true)
		title.Set("ws-k7", "")
		title.Set("ws-k7", "第一条消息")
		assert.Eq(t, 1, strings.Count(buf.String(), titleSaveSequence))
		assert.Eq(t, 2, strings.Count(buf.String(), "\x1b]0;"))
	})
	t.Run("an unchanged title is not repeated", func(t *testing.T) {
		var buf bytes.Buffer
		title := newTerminalTitle(&buf, &buf, true)
		title.Set("ws-k7", "你好")
		first := buf.String()
		title.Set("ws-k7", "你好")
		assert.Eq(t, first, buf.String())
	})
	t.Run("a title with a control character can not inject a sequence", func(t *testing.T) {
		var buf bytes.Buffer
		title := newTerminalTitle(&buf, &buf, true)
		title.Set("ws-k7", "a\x1b]2;pwned\x07")
		got := buf.String()
		assert.Eq(t, 1, strings.Count(got, "\x07"))
		assert.Eq(t, 1, strings.Count(got, "\x1b]0;"))
		assert.StrContains(t, got, "a]2;pwned")
	})
}

// TestTerminalTitleDisabled: with the switch off (or on a non-terminal, which
// chat reports the same way) no escape sequence is written at all.
func TestTerminalTitleDisabled(t *testing.T) {
	var buf bytes.Buffer
	title := newTerminalTitle(&buf, &buf, false)
	title.Set("ws-k7", "你好")
	title.Restore()
	assert.Eq(t, "", buf.String())
}

// TestTerminalTitleRestore: leaving chat clears the title (terminals without a
// title stack then show their own default instead of "suga · ..." for good) and
// pops the saved one after it.
func TestTerminalTitleRestore(t *testing.T) {
	t.Run("clear then pop the saved title", func(t *testing.T) {
		var buf bytes.Buffer
		title := newTerminalTitle(&buf, &buf, true)
		title.Set("ws-k7", "你好")
		buf.Reset()
		title.Restore()
		assert.Eq(t, titleClear+titleRestoreSequence, buf.String())
	})
	t.Run("twice writes nothing the second time", func(t *testing.T) {
		var buf bytes.Buffer
		title := newTerminalTitle(&buf, &buf, true)
		title.Set("ws-k7", "你好")
		title.Restore()
		buf.Reset()
		title.Restore()
		assert.Eq(t, "", buf.String())
	})
	t.Run("without a title there is nothing to restore", func(t *testing.T) {
		var buf bytes.Buffer
		title := newTerminalTitle(&buf, &buf, true)
		title.Restore()
		assert.Eq(t, "", buf.String())
	})
	t.Run("a title can be set again after restoring", func(t *testing.T) {
		var buf bytes.Buffer
		title := newTerminalTitle(&buf, &buf, true)
		title.Set("ws-k7", "你好")
		title.Restore()
		buf.Reset()
		title.Set("ws-k7", "你好")
		assert.Eq(t, titleSaveSequence+"\x1b]0;suga · ws-k7 · 你好\x07", buf.String())
	})
}

// TestChatTerminalTitleTiming: chat names the terminal title when it starts and
// updates it with the first message of a new session (the stored title), while a
// continued session starts with its stored title.
func TestChatTerminalTitleTiming(t *testing.T) {
	t.Run("new session: set at startup, updated with the first message", func(t *testing.T) {
		runner, _ := newCLITestRunner(t, mock.NewProvider(mock.Final("好的")))
		var stdout, stderr, title bytes.Buffer
		assert.NoErr(t, Run(context.Background(), runner,
			Options{TerminalTitle: true, terminalTitleOut: &title},
			strings.NewReader("帮我改一下标题\n:quit\n"), &stdout, &stderr, nil))

		got := title.String()
		// Set when chat knows the session, then updated with the first message (the
		// same title the session stores).
		assert.StrContains(t, got, "\x1b]0;suga")
		assert.StrContains(t, got, "· "+personal.SessionTitle("帮我改一下标题")+"\x07")
		// Leaving chat gives the terminal its own title back.
		assert.StrContains(t, got, titleRestoreSequence)
	})
	t.Run("resumed session: its stored title names the terminal from the start", func(t *testing.T) {
		runner, repo := newCLITestRunner(t, mock.NewProvider(mock.Final("好的")))
		ctx := context.Background()
		var stdout, stderr bytes.Buffer
		assert.NoErr(t, Run(ctx, runner, Options{}, strings.NewReader("第一次的问题\n:quit\n"), &stdout, &stderr, nil))
		sessions, err := repo.ListSessions(ctx)
		assert.NoErr(t, err)
		assert.Require(t, assert.Eq(t, 1, len(sessions)))

		var title bytes.Buffer
		stdout.Reset()
		assert.NoErr(t, Run(ctx, runner, Options{ResumeID: sessions[0].ID, TerminalTitle: true, terminalTitleOut: &title},
			strings.NewReader(":quit\n"), &stdout, &stderr, nil))
		assert.StrContains(t, title.String(), "· 第一次的问题\x07")
	})
	t.Run("switch off: nothing is written", func(t *testing.T) {
		runner, _ := newCLITestRunner(t, mock.NewProvider(mock.Final("好的")))
		var stdout, stderr, title bytes.Buffer
		assert.NoErr(t, Run(context.Background(), runner,
			Options{terminalTitleOut: &title},
			strings.NewReader("你好\n:quit\n"), &stdout, &stderr, nil))
		assert.Eq(t, "", title.String())
	})
	t.Run("non-terminal output: nothing is written", func(t *testing.T) {
		runner, _ := newCLITestRunner(t, mock.NewProvider(mock.Final("好的")))
		var stdout, stderr bytes.Buffer
		assert.NoErr(t, Run(context.Background(), runner,
			Options{TerminalTitle: true},
			strings.NewReader("你好\n:quit\n"), &stdout, &stderr, nil))
		assert.StrNotContains(t, stdout.String(), "\x1b]0;")
		assert.StrNotContains(t, stdout.String(), titleSaveSequence)
	})
}
