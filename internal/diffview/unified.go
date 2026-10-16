package diffview

import (
	"strconv"
	"strings"
)

// Kind distinguishes the lines of a unified diff.
type Kind uint8

const (
	// Hunk is a hunk header ("@@ -12,3 +12,4 @@").
	Hunk Kind = iota + 1
	// Context is a line the change left alone.
	Context
	// Add is a line the change adds.
	Add
	// Del is a line the change removes.
	Del
)

// Line is one line of a unified diff.
type Line struct {
	Kind Kind
	// Text is the line as it reads: the content of a context, added or removed
	// line, or the header of a hunk line.
	Text string
	// OldNo and NewNo are the numbers the line has in the old and in the new
	// file (0: that side has no such line). On a hunk line they are the first
	// numbers the hunk covers.
	OldNo, NewNo int
}

// Number is the number to show next to a diff line: the new file for an added
// line, the old file otherwise - the numbering a unified diff's hunks follow.
func (l Line) Number() int {
	if l.Kind == Add {
		return l.NewNo
	}
	return l.OldNo
}

// contextLines is how many unchanged lines surround each change (git's default).
const contextLines = 3

// Lines returns the unified diff of the change: every change with contextLines
// unchanged lines around it, changes closer than that merged into one hunk. It
// is nil when the change shows no text (a removal only reports RemovedLines).
func (c *Change) Lines() []Line {
	if c == nil || c.Old == nil || c.New == nil {
		return nil
	}
	return diffLines(splitLines(*c.Old), splitLines(*c.New), contextLines)
}

// diffLines builds the unified diff of two line lists: every change with
// context unchanged lines around it, changes closer than twice the context
// merged into one hunk (the shape git produces).
func diffLines(a, b []string, context int) []Line {
	ops := diffOps(a, b)
	var changed []int
	for i, o := range ops {
		if o.kind != ' ' {
			changed = append(changed, i)
		}
	}
	if len(changed) == 0 {
		return nil // identical: nothing to show
	}
	var out []Line
	for i := 0; i < len(changed); {
		start := changed[i] - context
		if start < 0 {
			start = 0
		}
		end := changed[i] + 1
		// A further change within twice the context belongs to the same hunk.
		j := i
		for j+1 < len(changed) && changed[j+1]-context <= end+context {
			j++
			end = changed[j] + 1
		}
		if end += context; end > len(ops) {
			end = len(ops)
		}
		out = append(out, hunk(ops, a, b, start, end)...)
		i = j + 1
	}
	return out
}

// hunk renders ops[start:end] as one hunk: its header, then its lines.
func hunk(ops []op, a, b []string, start, end int) []Line {
	lines := make([]Line, 0, end-start+1)
	lines = append(lines, Line{Kind: Hunk, Text: hunkHeader(ops, start, end)})
	for _, o := range ops[start:end] {
		switch o.kind {
		case ' ':
			lines = append(lines, Line{Kind: Context, Text: a[o.oldNo-1], OldNo: o.oldNo, NewNo: o.newNo})
		case '-':
			lines = append(lines, Line{Kind: Del, Text: a[o.oldNo-1], OldNo: o.oldNo})
		default:
			lines = append(lines, Line{Kind: Add, Text: b[o.newNo-1], NewNo: o.newNo})
		}
	}
	return lines
}

// hunkHeader is the header of one hunk: the first line number and the line count
// of each side. A side with no line at all (a file written from nothing) starts
// at 0, which keeps the "@@ -0,0 +1,42 @@" a new file gets; the hunk always
// carries at least one context line, so a count of zero means an empty side.
func hunkHeader(ops []op, start, end int) string {
	var oldStart, oldCount, newStart, newCount int
	for _, o := range ops[start:end] {
		if o.kind != '+' {
			oldCount++
			if oldStart == 0 {
				oldStart = o.oldNo
			}
		}
		if o.kind != '-' {
			newCount++
			if newStart == 0 {
				newStart = o.newNo
			}
		}
	}
	return "@@ -" + hunkSide(oldStart, oldCount) + " +" + hunkSide(newStart, newCount) + " @@"
}

// hunkSide renders one side of a hunk header: a single number when the range is
// one line long ("-12"), "start,count" otherwise ("-12,3", "-0,0").
func hunkSide(start, count int) string {
	if count == 1 {
		return strconv.Itoa(start)
	}
	return strconv.Itoa(start) + "," + strconv.Itoa(count)
}

// op is one line of a diff script: what happens to the line at oldNo/newNo
// (' ' unchanged, '-' removed, '+' added; the side a line is not in stays 0).
type op struct {
	kind         byte
	oldNo, newNo int
}

// diffOps builds the diff script of two line lists: the common head and tail
// stay context, the part between them is diffed by longest common subsequence.
func diffOps(a, b []string) []op {
	head, tail := commonEdges(a, b)
	ops := make([]op, 0, len(a)+len(b))
	for i := 0; i < head; i++ {
		ops = append(ops, op{kind: ' ', oldNo: i + 1, newNo: i + 1})
	}
	ops = append(ops, lcsOps(a[head:len(a)-tail], b[head:len(b)-tail], head)...)
	for i := 0; i < tail; i++ {
		ops = append(ops, op{kind: ' ', oldNo: len(a) - tail + i + 1, newNo: len(b) - tail + i + 1})
	}
	return ops
}

// commonEdges counts the lines two lists share at their start and at their end
// (the two counts never overlap).
func commonEdges(a, b []string) (head, tail int) {
	for head < len(a) && head < len(b) && a[head] == b[head] {
		head++
	}
	for tail < len(a)-head && tail < len(b)-head && a[len(a)-1-tail] == b[len(b)-1-tail] {
		tail++
	}
	return head, tail
}

// maxLCSCells caps the table lcsOps builds (the lines of one side times the
// other). A larger change is reported as all removed and all added: it reads
// the same for a person and keeps one pathological edit cheap.
const maxLCSCells = 250_000

// lcsOps diffs two line lists by their longest common subsequence, numbering the
// lines from base (the unchanged head in front of them).
func lcsOps(a, b []string, base int) []op {
	if len(a)*len(b) > maxLCSCells {
		return replaceOps(a, b, base)
	}
	n, m := len(a), len(b)
	stride := m + 1
	// table[i*stride+j] is the length of the common subsequence of a[i:] and b[j:].
	table := make([]uint32, (n+1)*stride)
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				table[i*stride+j] = table[(i+1)*stride+j+1] + 1
				continue
			}
			if down, right := table[(i+1)*stride+j], table[i*stride+j+1]; down >= right {
				table[i*stride+j] = down
			} else {
				table[i*stride+j] = right
			}
		}
	}
	ops := make([]op, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{kind: ' ', oldNo: base + i + 1, newNo: base + j + 1})
			i, j = i+1, j+1
		case table[(i+1)*stride+j] >= table[i*stride+j+1]:
			// Ties go to the removal, so a changed block reads as its old lines
			// followed by its new ones instead of the two interleaved.
			ops = append(ops, op{kind: '-', oldNo: base + i + 1})
			i++
		default:
			ops = append(ops, op{kind: '+', newNo: base + j + 1})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{kind: '-', oldNo: base + i + 1})
	}
	for ; j < m; j++ {
		ops = append(ops, op{kind: '+', newNo: base + j + 1})
	}
	return ops
}

// replaceOps reports a whole block as removed and then added.
func replaceOps(a, b []string, base int) []op {
	ops := make([]op, 0, len(a)+len(b))
	for i := range a {
		ops = append(ops, op{kind: '-', oldNo: base + i + 1})
	}
	for j := range b {
		ops = append(ops, op{kind: '+', newNo: base + j + 1})
	}
	return ops
}

// splitLines splits a text into the lines a diff counts: a trailing newline ends
// the last line instead of adding an empty one, an empty text has no line, and a
// CRLF file keeps no carriage return in what is shown.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	return lines
}
