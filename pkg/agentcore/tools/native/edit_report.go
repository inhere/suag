package native

import (
	"fmt"
	"go/parser"
	"go/scanner"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

const (
	// editContextLines surround each edited region in the file_edit result.
	editContextLines = 2
	// editSnippetLines caps the lines a file_edit result shows.
	editSnippetLines = 60
)

// editSnippet renders the edited regions of content (spans of the new text)
// as numbered lines with a little context, so the model sees the result and
// needs no re-read. Regions close together are merged; the total is capped.
func editSnippet(content string, spans []span) string {
	if len(spans) == 0 {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	// starts[i] is the byte offset of line i in content (CRLF counted).
	starts := make([]int, 0, len(lines))
	off := 0
	for _, l := range strings.SplitAfter(content, "\n") {
		starts = append(starts, off)
		off += len(l)
	}
	lineOf := func(pos int) int {
		return max(sort.SearchInts(starts, pos+1)-1, 0)
	}
	type rng struct{ from, to int }
	var ranges []rng
	for _, sp := range spans {
		end := max(sp.end-1, sp.start)
		ranges = append(ranges, rng{max(lineOf(sp.start)-editContextLines, 0), min(lineOf(end)+editContextLines, len(lines)-1)})
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].from < ranges[j].from })
	merged := []rng{ranges[0]}
	for _, r := range ranges[1:] {
		last := &merged[len(merged)-1]
		if r.from <= last.to+1 {
			last.to = max(last.to, r.to)
			continue
		}
		merged = append(merged, r)
	}
	var b strings.Builder
	shown := 0
	for i, r := range merged {
		if i > 0 {
			b.WriteString("...\n")
		}
		for n := r.from; n <= r.to; n++ {
			if shown == editSnippetLines {
				fmt.Fprintf(&b, "[snippet capped at %d lines]\n", editSnippetLines)
				return strings.TrimRight(b.String(), "\n")
			}
			fmt.Fprintf(&b, "%d\t%s\n", n+1, lines[n])
			shown++
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// syntaxRegression reports a Go syntax error the edit introduced: the old
// content parsed and the new one does not. Other files, and files that did
// not parse before, give "".
func syntaxRegression(path, oldContent, newContent string) string {
	if !strings.EqualFold(filepath.Ext(path), ".go") {
		return ""
	}
	if _, err := parser.ParseFile(token.NewFileSet(), path, oldContent, parser.SkipObjectResolution); err != nil {
		return ""
	}
	_, err := parser.ParseFile(token.NewFileSet(), path, newContent, parser.SkipObjectResolution)
	if err == nil {
		return ""
	}
	if list, ok := err.(scanner.ErrorList); ok && len(list) > 0 {
		return fmt.Sprintf("warning: this edit introduced a Go syntax error at line %d: %s", list[0].Pos.Line, list[0].Msg)
	}
	return "warning: this edit introduced a Go syntax error: " + err.Error()
}

// matchLines lists the 1-based lines where needle starts in content (at most
// max, then "...").
func matchLines(content, needle string, max int) string {
	var lines []string
	rest, offset := content, 0
	for len(lines) < max {
		idx := strings.Index(rest, needle)
		if idx < 0 {
			break
		}
		pos := offset + idx
		lines = append(lines, fmt.Sprint(strings.Count(content[:pos], "\n")+1))
		rest, offset = rest[idx+len(needle):], pos+len(needle)
	}
	out := "lines " + strings.Join(lines, ", ")
	if strings.Count(content, needle) > len(lines) {
		out += ", ..."
	}
	return out
}

// closestMatchHint explains a failed old_string: when its first line exists
// with other indentation it says so, otherwise it names the most similar
// line and shows the lines there, so the model can fix old_string without a
// re-read. "" when nothing is close.
func closestMatchHint(content, oldStr string) string {
	first := ""
	for _, l := range strings.Split(strings.ReplaceAll(oldStr, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			first = l
			break
		}
	}
	if first == "" {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	want := strings.TrimSpace(first)
	best, bestScore := -1, 0.0
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == want {
			best, bestScore = i, 2 // same text, other whitespace
			break
		}
		if score := tokenSimilarity(trimmed, want); score > bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 || bestScore < 0.5 {
		return "; re-read the part you are editing and copy the text exactly (whitespace and indentation included)"
	}
	n := len(strings.Split(strings.TrimRight(strings.ReplaceAll(oldStr, "\r\n", "\n"), "\n"), "\n"))
	to := min(best+max(n, 1)-1+editContextLines, len(lines)-1, best+11)
	var b strings.Builder
	if bestScore == 2 {
		fmt.Fprintf(&b, "; its first line appears at line %d with different whitespace or indentation. The file has:\n", best+1)
	} else {
		fmt.Fprintf(&b, "; the closest line is %d. The file has:\n", best+1)
	}
	for i := max(best-1, 0); i <= to; i++ {
		fmt.Fprintf(&b, "%d\t%s\n", i+1, lines[i])
	}
	b.WriteString("Copy old_string exactly from these lines.")
	return b.String()
}

// tokenSimilarity is the share of identifier/number tokens two lines have in
// common (Jaccard), a cheap measure for "the same line, slightly changed".
func tokenSimilarity(a, b string) float64 {
	split := func(s string) map[string]bool {
		set := map[string]bool{}
		for _, t := range strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' }) {
			set[t] = true
		}
		return set
	}
	ta, tb := split(a), split(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	common := 0
	for t := range ta {
		if tb[t] {
			common++
		}
	}
	return float64(common) / float64(len(ta)+len(tb)-common)
}
