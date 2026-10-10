package native

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gookit/goutil/x/assert"
)

func TestTruncateHeadTail(t *testing.T) {
	t.Run("short text is unchanged", func(t *testing.T) {
		assert.Eq(t, "hello", TruncateHeadTail("hello", 100))
	})
	t.Run("keeps head and tail and notes omitted bytes", func(t *testing.T) {
		s := strings.Repeat("a", 50) + strings.Repeat("b", 100) + strings.Repeat("c", 50)
		out := TruncateHeadTail(s, 100)
		assert.True(t, strings.HasPrefix(out, strings.Repeat("a", 50)))
		assert.True(t, strings.HasSuffix(out, strings.Repeat("c", 50)))
		assert.StrContains(t, out, "省略 100 字节")
	})
	t.Run("never splits a multi-byte rune", func(t *testing.T) {
		s := strings.Repeat("中文", 100) // 600 bytes
		for _, limit := range []int{7, 10, 31, 100} {
			out := TruncateHeadTail(s, limit)
			assert.True(t, utf8.ValidString(out), "limit %d", limit)
		}
	})
	t.Run("zero limit uses the default", func(t *testing.T) {
		s := strings.Repeat("x", DefaultMaxOutputBytes+10)
		assert.StrContains(t, TruncateHeadTail(s, 0), "省略 10 字节")
	})
}

func TestTruncateLines(t *testing.T) {
	t.Run("fits", func(t *testing.T) {
		out, n, cut := TruncateLines("a\nb\n", 100)
		assert.Eq(t, "a\nb\n", out)
		assert.Eq(t, 3, n)
		assert.False(t, cut)
	})
	t.Run("cuts at a line boundary", func(t *testing.T) {
		out, n, cut := TruncateLines("line1\nline2\nline3\n", 14)
		assert.Eq(t, "line1\nline2\n", out)
		assert.Eq(t, 2, n)
		assert.True(t, cut)
	})
	t.Run("one long line is cut on a rune boundary", func(t *testing.T) {
		out, _, cut := TruncateLines(strings.Repeat("中", 10), 10)
		assert.True(t, cut)
		assert.Eq(t, strings.Repeat("中", 3), out)
	})
}
