package termtext

import (
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestCleanRemovesTerminalControls(t *testing.T) {
	for _, tc := range []struct {
		name, control string
	}{
		{"OSC 52 clipboard", "\x1b]52;c;QVVESVQ=\a"},
		{"OSC 8 hyperlink", "\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\"},
		{"CSI clear screen", "\x1b[2J"},
		{"CSI cursor movement", "\x1b[12;34H"},
		{"BEL", "\a"},
		{"C1 CSI", "\x9b2J"},
		{"DCS", "\x1bPpayload\x1b\\"},
		{"APC", "\x1b_payload\x1b\\"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := "beforeafter"
			if tc.name == "OSC 8 hyperlink" {
				want = "beforelinkafter"
			}
			assert.Eq(t, want, Clean("before"+tc.control+"after"))
		})
	}
}

func TestCleanKeepsTextAndSafeWhitespace(t *testing.T) {
	t.Run("keeps newline and tab", func(t *testing.T) {
		assert.Eq(t, "a\nb\tcde", Clean("a\r\nb\t\x00c\x7fde"))
	})
	t.Run("keeps unicode", func(t *testing.T) {
		assert.Eq(t, "界 👩‍💻", Clean("界 👩‍💻"))
		assert.Eq(t, "", Clean("\x1b"))
	})
}

func TestCleanerHandlesSplitEscapeSequences(t *testing.T) {
	var cleaner Cleaner
	got := cleaner.Write("before\x1b]52;c;clipboard")
	got += cleaner.Write(" data")
	got += cleaner.Write("\aafter\x1b[2")
	got += cleaner.Write("Jdone")
	got += cleaner.Flush()
	assert.Eq(t, "beforeafterdone", got)
}

func TestCleanerDoesNotCarryAnIncompleteSequenceIntoTheNextMessage(t *testing.T) {
	var cleaner Cleaner
	assert.Eq(t, "text", cleaner.Write("text\x1b"))
	assert.Eq(t, "", cleaner.Flush())
	assert.Eq(t, "next", cleaner.Write("next"))
}
