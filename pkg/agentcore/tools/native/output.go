package native

import (
	"strings"
	"unicode/utf8"

	"github.com/inhere/suag/pkg/agentcore/tools"
)

// DefaultMaxOutputBytes is the default inline threshold (tools.max_output_bytes):
// file_read pages its own output to this size, and the output layer
// (tools.NewOutput) externalises any other result larger than it.
const DefaultMaxOutputBytes = tools.DefaultInlineOutputBytes

// DefaultRawOutputBytes is the default cap on the raw output of every other
// native tool (tools.max_spill_bytes). It bounds what the output layer may
// save to the session directory, not what enters the context.
const DefaultRawOutputBytes = tools.DefaultRawOutputBytes

// outputLimit returns limit, or the inline default when limit is not positive.
func outputLimit(limit int) int {
	if limit <= 0 {
		return DefaultMaxOutputBytes
	}
	return limit
}

// rawLimit returns limit, or the raw-output default when limit is not positive.
func rawLimit(limit int) int {
	if limit <= 0 {
		return DefaultRawOutputBytes
	}
	return limit
}

// utf8Cut moves cut back so that s[:cut] does not split a multi-byte rune.
func utf8Cut(s string, cut int) int {
	if cut >= len(s) {
		return len(s)
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return cut
}

// TruncateHeadTail keeps the head and tail of s within limit bytes (UTF-8
// safe) and notes how many bytes were omitted in between; limit <= 0 uses
// DefaultMaxOutputBytes. See tools.TruncateHeadTail.
func TruncateHeadTail(s string, limit int) string {
	return tools.TruncateHeadTail(s, outputLimit(limit))
}

// TruncateLines keeps whole lines of s while they fit in limit bytes. It
// returns the kept text, the number of kept lines and whether anything was
// dropped. A single line longer than limit is cut on a UTF-8 boundary so at
// least some content comes back.
func TruncateLines(s string, limit int) (string, int, bool) {
	limit = outputLimit(limit)
	if len(s) <= limit {
		return s, strings.Count(s, "\n") + 1, false
	}
	cut := strings.LastIndexByte(s[:limit], '\n')
	if cut < 0 {
		return s[:utf8Cut(s, limit)], 1, true
	}
	kept := s[:cut+1]
	return kept, strings.Count(kept, "\n"), true
}
