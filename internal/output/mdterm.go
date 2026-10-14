package output

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/gookit/color"
)

// Terminal markdown rendering, used by chat only. The model answers in
// markdown; on a terminal that answer is shown as a plain-text approximation of
// it instead of the raw markers. `suga run`, stream-json, ACP and the transcript
// keep the exact text.
//
// Rendering is line based: MDTerm buffers the text after the last newline and
// renders it as soon as its newline (or the end of the message) arrives, so a
// long answer appears while it streams and a marker split in half by a delta
// boundary is still rendered whole.

const (
	// mdRuleWidth is the number of ─ characters drawn for a horizontal rule.
	mdRuleWidth = 40
	// mdFence is the short line that replaces a code fence; an opening fence
	// adds the language it names.
	mdFence = "───"
)

// Terminal styles of the markdown elements.
var (
	mdHeadingStyle = color.Style{color.OpBold, color.FgCyan}
	mdDimStyle     = color.Style{color.FgGray}
	mdCodeStyle    = color.Style{color.FgLightYellow}
)

// List items: the indent, the marker and the space behind it are kept, only the
// bullet of an unordered list is replaced. The marker needs a space behind it,
// so "**bold**" and "*italic*" are not read as list items.
var (
	mdBulletItem   = regexp.MustCompile(`^([ \t]*)([-*+])([ \t]+)(.*)$`)
	mdNumberedItem = regexp.MustCompile(`^([ \t]*)(\d{1,9}[.)])([ \t]+)(.*)$`)
)

// MDTerm renders markdown text line by line to a terminal writer. Zero value is
// not usable; call NewMDTerm.
type MDTerm struct {
	w       io.Writer
	color   bool
	pending string // text after the last newline, not rendered yet
	block   bool   // inside a fenced code block; spans the lines of one message
	// prefix, when set, is printed at the start of every message's first line: a
	// caller that cannot write the prefix itself (it would be a write without a
	// newline while an input line is on screen, see LiveText.SetWholeLines) has the
	// renderer, which knows where a line starts, do it.
	prefix  string
	started bool // the current message already printed its prefix
}

// NewMDTerm returns an MDTerm writing rendered lines to w; color styles them.
func NewMDTerm(w io.Writer, color bool) *MDTerm { return &MDTerm{w: w, color: color} }

// SetLinePrefix sets the text printed at the start of every message's first line
// ("" removes it).
func (t *MDTerm) SetLinePrefix(prefix string) { t.prefix = prefix }

// Write takes the next chunk of markdown text and prints every line it
// completes; the rest waits in the buffer for its newline.
func (t *MDTerm) Write(text string) {
	if text == "" {
		return
	}
	t.pending += text
	for {
		i := strings.IndexByte(t.pending, '\n')
		if i < 0 {
			return
		}
		line := t.pending[:i]
		t.pending = t.pending[i+1:]
		t.print(line)
	}
}

// Print renders text that arrives as one piece (a final answer that was not
// streamed) and ends it on a line of its own. Empty text only ends the line its
// prefix was written on.
func (t *MDTerm) Print(text string) {
	if text == "" {
		fmt.Fprintln(t.w)
		return
	}
	t.Write(text)
	if t.pending != "" {
		t.Flush()
	}
}

// Flush renders the buffered, still incomplete line and ends the message: a
// code block left open does not carry into the next message.
func (t *MDTerm) Flush() {
	if line := t.pending; line != "" {
		t.pending = ""
		t.print(line)
	}
	t.block = false
	// The next message gets the line prefix again.
	t.started = false
}

func (t *MDTerm) print(line string) {
	rendered := t.renderLine(line)
	if t.prefix != "" && !t.started {
		rendered, t.started = t.prefix+rendered, true
	}
	fmt.Fprintln(t.w, rendered)
}

// renderLine renders one complete markdown line, without its newline.
func (t *MDTerm) renderLine(s string) string {
	trimmed := strings.TrimSpace(s)
	// Fences first: inside a code block nothing else is interpreted.
	if strings.HasPrefix(trimmed, "```") {
		info := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
		switch {
		case !t.block:
			t.block = true
			if info != "" {
				return t.paint(mdDimStyle, mdFence+" "+info)
			}
			return t.paint(mdDimStyle, mdFence)
		case info == "":
			t.block = false
			return t.paint(mdDimStyle, mdFence)
		}
		// A "```lang" line inside the block is content, not a closing fence.
	}
	if t.block {
		return t.paint(mdDimStyle, s) // code keeps its exact text and is dimmed
	}
	if mdRule(trimmed) {
		return t.paint(mdDimStyle, strings.Repeat("─", mdRuleWidth))
	}
	if _, text, ok := mdHeading(trimmed); ok {
		return t.inline(text, mdHeadingStyle)
	}
	if strings.HasPrefix(trimmed, "|") {
		return s // a table stays exactly as the model wrote it
	}
	if text, depth, ok := mdQuote(s); ok {
		return t.paint(mdDimStyle, strings.Repeat("│ ", depth)) + t.inline(text, mdDimStyle)
	}
	if m := mdBulletItem.FindStringSubmatch(s); m != nil {
		return m[1] + "•" + m[3] + t.inline(m[4], nil)
	}
	if m := mdNumberedItem.FindStringSubmatch(s); m != nil {
		return m[1] + m[2] + m[3] + t.inline(m[4], nil)
	}
	return t.inline(s, nil)
}

// inline renders the inline markers of one line: `code`, **bold**, *italic*,
// _italic_ and [text](url). A marker without its partner is printed as it is,
// and code keeps its content untouched. base is the style of the plain text
// around the markers (the dim of a quote line, the heading style, ...); every
// styled part carries base plus its own marker style, so nesting keeps working.
func (t *MDTerm) inline(s string, base color.Style) string {
	var out, plain strings.Builder
	flush := func() {
		if plain.Len() > 0 {
			out.WriteString(t.paint(base, plain.String()))
			plain.Reset()
		}
	}
	for i := 0; i < len(s); {
		rest := s[i:]
		switch {
		case rest[0] == '`':
			if j := strings.IndexByte(rest[1:], '`'); j >= 0 {
				flush()
				out.WriteString(t.paint(mdCodeStyle, rest[1:1+j]))
				i += j + 2
				continue
			}
		case strings.HasPrefix(rest, "**"):
			if j := strings.Index(rest[2:], "**"); j > 0 {
				flush()
				out.WriteString(t.inline(rest[2:2+j], mdWithStyle(base, color.OpBold)))
				i += j + 4
				continue
			}
		case rest[0] == '*':
			if j := strings.IndexByte(rest[1:], '*'); j > 0 {
				flush()
				out.WriteString(t.inline(rest[1:1+j], mdWithStyle(base, color.OpItalic)))
				i += j + 2
				continue
			}
		case rest[0] == '_':
			if end := mdUnderscoreClose(s, i); end > 0 {
				flush()
				out.WriteString(t.inline(s[i+1:end], mdWithStyle(base, color.OpItalic)))
				i = end + 1
				continue
			}
		case rest[0] == '[':
			if text, url, n, ok := mdLink(rest); ok {
				flush()
				out.WriteString(t.paint(base, text) + " (" + t.paint(base, url) + ")")
				i += n
				continue
			}
		}
		plain.WriteByte(s[i])
		i++
	}
	flush()
	return out.String()
}

// paint styles s with style, or returns it plain when colors are off (an empty
// style is plain as well).
func (t *MDTerm) paint(style color.Style, s string) string {
	if !t.color || len(style) == 0 {
		return s
	}
	return style.Render(s)
}

// mdWithStyle returns base with extra styles added (a new slice: base may be
// shared between the parts of a line).
func mdWithStyle(base color.Style, extra ...color.Color) color.Style {
	style := make(color.Style, 0, len(base)+len(extra))
	style = append(style, base...)
	return append(style, extra...)
}

// mdHeading reports the level of an ATX heading ("## Title") and its text.
// "#x" is not a heading.
func mdHeading(s string) (level int, text string, ok bool) {
	for level < len(s) && level < 6 && s[level] == '#' {
		level++
	}
	if level == 0 || (level < len(s) && s[level] != ' ' && s[level] != '\t') {
		return 0, "", false
	}
	return level, strings.TrimSpace(s[level:]), true
}

// mdRule reports whether the trimmed line is a horizontal rule ("---", "***").
func mdRule(s string) bool {
	if len(s) < 3 {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] != s[0] {
			return false
		}
	}
	return s[0] == '-' || s[0] == '*' || s[0] == '_'
}

// mdQuote splits a blockquote line into its text and how many ">" markers it
// has (they may nest and may be indented).
func mdQuote(s string) (text string, depth int, ok bool) {
	rest := s
	for {
		trimmed := strings.TrimLeft(rest, " \t")
		if !strings.HasPrefix(trimmed, ">") {
			break
		}
		rest = strings.TrimPrefix(strings.TrimPrefix(trimmed, ">"), " ")
		depth++
	}
	if depth == 0 {
		return "", 0, false
	}
	return rest, depth, true
}

// mdUnderscoreClose returns the index (in s) of the "_" closing an emphasis that
// opens at s[i], or -1. Underscores inside a word (foo_bar_baz) are no emphasis,
// so identifiers in an answer are not italicized.
func mdUnderscoreClose(s string, i int) int {
	if i > 0 && mdWordByte(s[i-1]) {
		return -1
	}
	j := strings.IndexByte(s[i+1:], '_')
	if j <= 0 { // no "…_", or an empty "__" pair
		return -1
	}
	end := i + 1 + j
	if end+1 < len(s) && mdWordByte(s[end+1]) {
		return -1
	}
	return end
}

// mdWordByte reports whether b can be part of a word.
func mdWordByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// mdLink parses "[text](url)" at the start of s and reports how many bytes it
// takes; ok is false when the line has no complete link there.
func mdLink(s string) (text, url string, n int, ok bool) {
	if len(s) < 4 || s[0] != '[' {
		return "", "", 0, false
	}
	end := strings.IndexByte(s, ']')
	if end < 0 || end+1 >= len(s) || s[end+1] != '(' {
		return "", "", 0, false
	}
	close := strings.IndexByte(s[end+2:], ')')
	if close < 0 {
		return "", "", 0, false
	}
	return s[1:end], s[end+2 : end+2+close], end + 2 + close + 1, true
}
