package llm

import (
	"bytes"
	"io"
)

// streamLines splits an event-stream body into lines for bufio.Scanner with one
// addition to bufio.ScanLines that the SSE readers need to tell two ends apart:
// "the stream ended after a complete line" (a normal end, or simply no end
// marker) and "the connection - a proxy or gateway in between included - closed
// in the middle of a line". The half line is not a chunk the provider sent but
// a cut answer; parsing it would fail the turn with "unexpected end of JSON
// input" (issue suga-zq46), so the readers report it as a truncated stream -
// a streamReadError, which retryStream requests again.
type streamLines struct {
	// cut is set when the last line of the body arrived without its newline
	// terminator; that line is still handed out as a token.
	cut bool
}

func newStreamLines() *streamLines { return &streamLines{} }

// split is the bufio.SplitFunc of streamLines: lines as bufio.ScanLines, except
// that an unterminated last line is returned once as its own token and the scan
// then ends with io.ErrUnexpectedEOF (scanner.Err), so a loop that ran into EOF
// reports the cut. A loop that ended on a terminator ([DONE], message_stop)
// reads no further and stays unaffected by it - a gateway that omits the last
// newline after a terminator is not treated as cut. truncated() covers the
// token of that cut line itself, whose JSON is usually incomplete already.
func (l *streamLines) split(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		line := data[:i]
		// Drop a trailing CR, as bufio.ScanLines does for \r\n line ends.
		if n := len(line); n > 0 && line[n-1] == '\r' {
			line = line[:n-1]
		}
		return i + 1, line, nil
	}
	if !atEOF {
		return 0, nil, nil // wait for more data
	}
	if len(data) == 0 {
		if l.cut {
			return 0, nil, io.ErrUnexpectedEOF // the cut line was the last one
		}
		return 0, nil, nil // the body ended cleanly
	}
	l.cut = true
	return len(data), data, nil
}

// truncated reports whether the line the reader is looking at was the last line
// of the body and had no newline terminator, i.e. the stream was cut there.
func (l *streamLines) truncated() bool { return l.cut }

// cutLine is the detail streamTruncatedError gets when the readers see a cut
// line (they cannot say more than this about it).
const cutLine = "the connection closed in the middle of an event line"
