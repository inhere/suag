package diffview

import (
	"strconv"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// linesOf is the diff of two texts, as a change carries it.
func linesOf(oldText, newText string) []Line {
	return (&Change{Old: &oldText, New: &newText}).Lines()
}

// kindOf names a diff line's kind in a failure message.
func kindOf(line Line) string {
	switch line.Kind {
	case Hunk:
		return "hunk"
	case Context:
		return "context"
	case Add:
		return "add"
	default:
		return "del"
	}
}

// shape renders diff lines as "kind:text:old:new", so a case can pin the whole
// diff in one assertion.
func shape(lines []Line) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		mark := map[Kind]string{Hunk: "h", Context: " ", Add: "+", Del: "-"}[line.Kind]
		out = append(out, mark+":"+line.Text+":"+strconv.Itoa(line.OldNo)+":"+strconv.Itoa(line.NewNo))
	}
	return out
}

func TestLinesEdit(t *testing.T) {
	oldText := "a\nb\nc\nd\ne\nf\ng\nh\n"
	newText := "a\nb\nc\nD\ne\nf\ng\nh\n"

	t.Run("one changed line keeps three lines of context", func(t *testing.T) {
		assert.Eq(t, []string{
			"h:@@ -1,7 +1,7 @@:0:0",
			" :a:1:1",
			" :b:2:2",
			" :c:3:3",
			"-:d:4:0",
			"+:D:0:4",
			" :e:5:5",
			" :f:6:6",
			" :g:7:7",
		}, shape(linesOf(oldText, newText)))
	})

	t.Run("the number to show follows the unified diff rule", func(t *testing.T) {
		numbers := map[string]int{}
		for _, line := range linesOf(oldText, newText) {
			if line.Kind != Hunk {
				numbers[kindOf(line)+" "+line.Text] = line.Number()
			}
		}
		assert.Eq(t, 3, numbers["context c"])
		assert.Eq(t, 4, numbers["del d"]) // the old file
		assert.Eq(t, 4, numbers["add D"]) // the new file
	})

	t.Run("an unchanged file has no diff", func(t *testing.T) {
		assert.Empty(t, linesOf(oldText, oldText))
	})

	t.Run("a change in the first line shows what follows it", func(t *testing.T) {
		lines := linesOf(oldText, "A\nb\nc\nd\ne\nf\ng\nh\n")
		assert.Eq(t, "@@ -1,4 +1,4 @@", lines[0].Text)
		assert.Eq(t, Hunk, lines[0].Kind)
		last := lines[len(lines)-1]
		assert.Eq(t, "context", kindOf(last))
		assert.Eq(t, "d", last.Text) // only the three lines after the change, not h
	})

	t.Run("a removal of lines shows both sides", func(t *testing.T) {
		assert.Eq(t, []string{
			"h:@@ -1,8 +1,3 @@:0:0",
			" :a:1:1",
			" :b:2:2",
			"-:c:3:0",
			"-:d:4:0",
			"-:e:5:0",
			"-:f:6:0",
			"-:g:7:0",
			" :h:8:3",
		}, shape(linesOf(oldText, "a\nb\nh\n")))
	})

	t.Run("a change without one of the texts has no diff", func(t *testing.T) {
		assert.Empty(t, (&Change{New: &newText}).Lines())
		assert.Empty(t, (&Change{Old: &oldText}).Lines())
		assert.Empty(t, (*Change)(nil).Lines())
	})
}

func TestLinesNewFile(t *testing.T) {
	assert.Eq(t, []string{
		"h:@@ -0,0 +1,3 @@:0:0",
		"+:one:0:1",
		"+:two:0:2",
		"+:three:0:3",
	}, shape(linesOf("", "one\ntwo\nthree\n")))
}

func TestLinesHunks(t *testing.T) {
	t.Run("distant changes become two hunks", func(t *testing.T) {
		body := make([]string, 40)
		for i := range body {
			body[i] = "line " + strconv.Itoa(i+1)
		}
		oldText := strings.Join(body, "\n") + "\n"
		body[0], body[29] = "changed 1", "changed 30"
		newText := strings.Join(body, "\n") + "\n"

		var headers []string
		for _, line := range linesOf(oldText, newText) {
			if line.Kind == Hunk {
				headers = append(headers, line.Text)
			}
		}
		assert.Eq(t, []string{"@@ -1,4 +1,4 @@", "@@ -27,7 +27,7 @@"}, headers)
	})

	t.Run("changes within twice the context share one hunk", func(t *testing.T) {
		body := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
		oldText := strings.Join(body, "\n") + "\n"
		body[0], body[6] = "A", "G" // six lines apart, 2*context: one hunk
		newText := strings.Join(body, "\n") + "\n"
		lines := linesOf(oldText, newText)
		assert.Eq(t, "@@ -1,8 +1,8 @@", lines[0].Text)
		assert.Eq(t, Hunk, lines[0].Kind)
		hunks := 0
		for _, line := range lines {
			if line.Kind == Hunk {
				hunks++
			}
		}
		assert.Eq(t, 1, hunks)
	})
}

// TestLinesReplacedBlockReadsOldThenNew pins that a changed block reads as its
// old lines followed by its new ones, never interleaved.
func TestLinesReplacedBlockReadsOldThenNew(t *testing.T) {
	assert.Eq(t, []string{
		"h:@@ -1,5 +1,5 @@:0:0",
		" :keep:1:1",
		"-:a:2:0",
		"-:b:3:0",
		"-:c:4:0",
		"+:x:0:2",
		"+:y:0:3",
		"+:z:0:4",
		" :tail:5:5",
	}, shape(linesOf("keep\na\nb\nc\ntail\n", "keep\nx\ny\nz\ntail\n")))
}

// TestLinesHugeChangeFallsBack: past the table limit the changed part is
// reported as removed and added as a whole instead of being compared line by
// line (the head still stays context).
func TestLinesHugeChangeFallsBack(t *testing.T) {
	oldLines := make([]string, 600)
	newLines := make([]string, 600)
	for i := range oldLines {
		oldLines[i] = "old " + strconv.Itoa(i)
		newLines[i] = "new " + strconv.Itoa(i)
	}
	oldText := strings.Join(oldLines, "\n") + "\n"
	newText := strings.Join(newLines, "\n") + "\n"
	lines := linesOf(oldText, newText)
	assert.Eq(t, "@@ -1,600 +1,600 @@", lines[0].Text)
	assert.Eq(t, "del", kindOf(lines[1]))
	assert.Eq(t, "old 0", lines[1].Text)
	assert.Eq(t, "add", kindOf(lines[len(lines)-1]))

	lines = linesOf("head\n"+oldText, "head\n"+newText)
	assert.Eq(t, "context", kindOf(lines[1]))
	assert.Eq(t, "head", lines[1].Text)
}

func TestSplitLines(t *testing.T) {
	for name, tc := range map[string]struct {
		text string
		want []string
	}{
		"empty":            {"", nil},
		"one line":         {"a", []string{"a"}},
		"trailing newline": {"a\nb\n", []string{"a", "b"}},
		"blank last line":  {"a\n\n", []string{"a", ""}},
		"crlf keeps no CR": {"a\r\nb\r\n", []string{"a", "b"}},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Eq(t, tc.want, splitLines(tc.text))
		})
	}
}
