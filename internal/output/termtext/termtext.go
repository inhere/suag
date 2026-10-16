// Package termtext removes terminal control sequences from text that came from
// a model, a tool or another external source before chat writes it to a terminal.
package termtext

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Cleaner consumes terminal text incrementally. Escape sequences can be split
// across model deltas, so callers must keep one Cleaner for one displayed text
// stream and call Flush when that stream ends.
type Cleaner struct {
	state   state
	pending string // an incomplete UTF-8 encoding at the end of a chunk
}

type state uint8

const (
	stateText state = iota
	stateEscape
	stateCSI
	stateString
	stateStringEscape
)

// Write removes C0/C1 controls and terminal escape sequences from value. Newline
// and tab are retained; every other control is removed rather than replaced so
// the terminal cannot interpret a visible-looking replacement as part of an
// escape sequence, and normal text is not made noisier by security markers.
func (c *Cleaner) Write(value string) string {
	if c.pending != "" {
		value = c.pending + value
		c.pending = ""
	}
	var out strings.Builder
	for i := 0; i < len(value); {
		if c.state != stateText {
			i = c.consumeSequence(value, i)
			continue
		}

		b := value[i]
		switch {
		case b == 0x1b:
			c.state = stateEscape
			i++
		case b >= 0x80 && b <= 0x9f:
			c.startC1(b)
			i++
		case b < 0x20 || b == 0x7f:
			if b == '\n' || b == '\t' {
				out.WriteByte(b)
			}
			i++
		case b < 0x80:
			out.WriteByte(b)
			i++
		default:
			r, size := utf8.DecodeRuneInString(value[i:])
			if r == utf8.RuneError && size == 1 {
				if !utf8.FullRuneInString(value[i:]) {
					c.pending = value[i:]
					return out.String()
				}
				// Invalid bytes cannot form an ASCII terminal command; preserve them
				// so cleaning does not silently corrupt otherwise printable output.
				out.WriteByte(b)
				i++
				continue
			}
			if !unicode.IsControl(r) {
				out.WriteString(value[i : i+size])
			}
			i += size
		}
	}
	return out.String()
}

// Flush ends the current stream. An incomplete escape sequence or UTF-8 byte
// sequence is discarded; it is not safe to carry it into the next message.
func (c *Cleaner) Flush() string {
	c.state = stateText
	c.pending = ""
	return ""
}

// Clean removes terminal controls from one complete value.
func Clean(value string) string {
	var cleaner Cleaner
	return cleaner.Write(value) + cleaner.Flush()
}

func (c *Cleaner) startC1(b byte) {
	switch b {
	case 0x9b: // CSI
		c.state = stateCSI
	case 0x9d, 0x90, 0x98, 0x9e, 0x9f: // OSC, DCS, SOS, PM, APC
		c.state = stateString
	default:
		// Other C1 controls are discarded as ordinary controls.
		c.state = stateText
	}
}

func (c *Cleaner) consumeSequence(value string, i int) int {
	b := value[i]
	switch c.state {
	case stateEscape:
		switch b {
		case '[':
			c.state = stateCSI
		case ']':
			c.state = stateString
		case 'P', 'X', '^', '_': // DCS, SOS, PM, APC
			c.state = stateString
		default:
			c.state = stateText
		}
		return i + 1
	case stateCSI:
		if b == 0x9c {
			c.state = stateText
		} else if b >= 0x40 && b <= 0x7e {
			c.state = stateText
		} else if b == 0x1b {
			c.state = stateEscape
		}
		return i + 1
	case stateString:
		if b == 0x07 || b == 0x9c {
			c.state = stateText
		} else if b == 0x1b {
			c.state = stateStringEscape
		}
		return i + 1
	case stateStringEscape:
		if b == '\\' || b == 0x9c {
			c.state = stateText
		} else if b != 0x1b {
			c.state = stateString
		}
		return i + 1
	default:
		c.state = stateText
		return i
	}
}

// startC1 and consumeSequence intentionally handle raw C1 bytes separately from
// UTF-8 continuation bytes: for example, U+0080 is encoded as C2 80 and must not
// be mistaken for a raw terminal C1 byte while decoding ordinary Unicode text.
