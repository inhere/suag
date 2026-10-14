package chat

import (
	"fmt"
	"io"
	"strings"
	"unicode"
)

// The window/tab title of the terminal chat runs in: OSC 0 sets it, and xterm's
// XTWINOPS coupling (CSI 22;0t / CSI 23;0t) saves the terminal's own title when
// chat takes it over and puts it back when chat ends. A terminal without that
// coupling ignores the two sequences; see Restore for what it then shows.
const (
	// titleProgram names the program the title belongs to.
	titleProgram = "suga"
	// titleLimit caps each part of the title, in runes: a tab label, not a
	// sentence. The short name and the session title are measured against it.
	titleLimit = 30
	// titleSep joins the parts of the title.
	titleSep = " · "
	// titleSetFormat is OSC 0, terminated by BEL: accepted by xterm, tmux, kitty,
	// Windows Terminal, Zed and the other usual terminals.
	titleSetFormat = "\x1b]0;%s\x07"
	// titleClear sets an empty title: the fallback for a terminal that honours
	// OSC 0 but keeps no title stack (see Restore).
	titleClear = "\x1b]0;\x07"
	// titleSaveSequence pushes the terminal's own title onto its title stack.
	titleSaveSequence = "\x1b[22;0t"
	// titleRestoreSequence pops it back.
	titleRestoreSequence = "\x1b[23;0t"
)

// terminalTitle keeps the terminal's window title in step with the chat session
// and gives the terminal its own title back when chat ends. Every sequence goes
// through the same writer as the rest of the chat output (the input source),
// never to the terminal file directly: the line editor serializes its writes, so
// a title can not land inside a prompt repaint.
type terminalTitle struct {
	out io.Writer
	// enabled is chat.terminal_title on a colored terminal. Off means chat writes
	// no escape sequence at all (a pipe, a redirected output, NO_COLOR).
	enabled bool
	// escape reports whether the host interprets escape sequences. A Windows
	// console without virtual terminal processing does not: the object would be
	// shown as plain text there, so the title goes through the console API
	// instead (title_windows.go).
	escape bool
	// native is the platform's own title call, set only where escape sequences
	// are not interpreted; best effort, its error changes nothing on screen.
	native func(string) error
	// pushed records that a title was written (and the terminal's own one saved
	// before it); title is the last title written.
	pushed bool
	title  string
}

// newTerminalTitle returns the title writer of one chat. device is the terminal
// the sequences are printed to (stdout); on Windows its console mode decides
// whether escape sequences are interpreted at all.
func newTerminalTitle(out, device io.Writer, enabled bool) *terminalTitle {
	title := &terminalTitle{out: out, enabled: enabled, escape: true}
	if enabled {
		title.escape, title.native = nativeTitleSupport(device)
	}
	return title
}

// terminalTitleWriter is where the title sequences go: the chat output, or the
// buffer a test injected to observe the title without a terminal.
func (o Options) terminalTitleWriter(fallback io.Writer) io.Writer {
	if o.terminalTitleOut != nil {
		return o.terminalTitleOut
	}
	return fallback
}

// terminalTitleEnabled reports whether chat may write title sequences: the
// switch on (chat.terminal_title) on a colored terminal. A test that injects an
// output buffer stands in for that terminal.
func (o Options) terminalTitleEnabled(color bool) bool {
	return o.TerminalTitle && (color || o.terminalTitleOut != nil)
}

// Set writes the title of the session: the program, the session's short name and
// the session title (either may be empty). The terminal's own title is saved
// before the first write, and a title unchanged since the last write is not
// repeated.
func (t *terminalTitle) Set(shortName, sessionTitle string) {
	t.write(titleFor(shortName, sessionTitle))
}

// write writes one title.
func (t *terminalTitle) write(title string) {
	if !t.enabled || title == t.title {
		return
	}
	if !t.pushed {
		t.pushed = true
		if t.escape {
			fmt.Fprint(t.out, titleSaveSequence)
		}
	}
	t.title = title
	if t.escape {
		fmt.Fprintf(t.out, titleSetFormat, title)
	}
	if t.native != nil {
		_ = t.native(title)
	}
}

// Restore gives the terminal its own title back: chat ends. Nothing was written
// when no title was ever set, so there is nothing to restore either.
//
// An empty title is written first and the saved one popped after it, so a
// terminal that honours OSC 0 but keeps no title stack (Windows Terminal, VS
// Code, Zed) ends up without a title - it then shows its own default, in the
// worst case the name of the shell or process, instead of keeping "suga · ..."
// for good. On a terminal that does keep the stack (xterm, tmux) the pop is what
// counts, and the title chat started with comes back.
func (t *terminalTitle) Restore() {
	if !t.pushed {
		return
	}
	if t.escape {
		fmt.Fprint(t.out, titleClear)
		fmt.Fprint(t.out, titleRestoreSequence)
	}
	if t.native != nil {
		_ = t.native("")
	}
	t.pushed, t.title = false, ""
}

// titleFor builds the terminal title of a chat: the program, the session's short
// name ("ws-k7", the address `suga send --session <name>` accepts) and the
// session title, joined with " · ". Empty parts are dropped, so a chat whose
// short name is not known yet (or whose session has no title yet) names what it
// has.
func titleFor(shortName, sessionTitle string) string {
	parts := []string{titleProgram}
	for _, part := range []string{shortName, sessionTitle} {
		if clean := cleanTitlePart(part); clean != "" {
			parts = append(parts, clean)
		}
	}
	return strings.Join(parts, titleSep)
}

// cleanTitlePart prepares one part of the title for the OSC sequence: control
// characters are dropped (inside OSC 0 an ESC or BEL ends the sequence early and
// lets the rest of the title through as terminal commands - an injected escape
// sequence), a newline or tab becomes one space, and the text is capped at
// titleLimit runes.
func cleanTitlePart(s string) string {
	var b strings.Builder
	pendingSpace := false
	for _, r := range s {
		if unicode.IsControl(r) {
			// IsControl covers C0 (ESC, BEL, \r, \n, ...), DEL and the C1 range.
			if r == '\n' || r == '\r' || r == '\t' {
				pendingSpace = b.Len() > 0
			}
			continue
		}
		if pendingSpace {
			b.WriteRune(' ')
			pendingSpace = false
		}
		b.WriteRune(r)
	}
	title := strings.TrimSpace(b.String())
	if runes := []rune(title); len(runes) > titleLimit {
		title = string(runes[:titleLimit])
	}
	return title
}
