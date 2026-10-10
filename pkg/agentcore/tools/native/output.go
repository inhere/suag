package native

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// DefaultMaxOutputBytes is the default cap on one tool result stored in the
// session (tools.max_output_bytes). It truncates the stored content for good,
// unlike the context builder's view-only trimming.
const DefaultMaxOutputBytes = 30000

// outputLimit returns limit, or the default when limit is not positive.
func outputLimit(limit int) int {
	if limit <= 0 {
		return DefaultMaxOutputBytes
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

// utf8CutFrom moves start forward so that s[start:] begins on a rune start.
func utf8CutFrom(s string, start int) int {
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return start
}

// TruncateHeadTail keeps the head and tail of s within limit bytes (UTF-8
// safe) and notes how many bytes were omitted in between. The tail usually
// holds errors and exit status, so both ends are kept.
func TruncateHeadTail(s string, limit int) string {
	limit = outputLimit(limit)
	if len(s) <= limit {
		return s
	}
	headLen := utf8Cut(s, limit/2)
	tailStart := utf8CutFrom(s, len(s)-(limit-headLen))
	omitted := tailStart - headLen
	return fmt.Sprintf("%s\n…[省略 %d 字节，输出上限 %d 字节]…\n%s", s[:headLen], omitted, limit, s[tailStart:])
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
