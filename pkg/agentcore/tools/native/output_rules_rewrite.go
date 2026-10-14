package native

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// This file holds the rule steps that shape the lines a rule keeps - replace
// rewrites the text, group_by merges lines sharing a key, head/tail keep the
// ends - and the rendering of a fold block's text. They run after
// drop/fold/block and before the global line cap (minimize.go): the rule first
// decides what matters, these steps then make it shorter.

// groupIndent is what a group_by body line is indented with.
const groupIndent = "  "

// applyReplace runs the rule's replace items over the text the rule kept. The
// patterns were compiled in multi-line mode (^/$ anchor on a line) and are
// applied in the order they are written, so a later item sees the result of the
// earlier ones.
func (c *compiledRule) applyReplace(lines []string) []string {
	if len(c.replaces) == 0 {
		return lines
	}
	text := strings.Join(lines, "\n")
	for _, item := range c.replaces {
		text = item.re.ReplaceAllString(text, item.with)
	}
	return strings.Split(text, "\n")
}

// groupBy merges neighbouring lines whose group_by key is the same: the key's
// whole match is printed once as the group header and the rest of every line of
// the group follows indented (the file path of a grep hit is written once). A
// key with a single line keeps its own shape - grouping it would only add a
// line, and a rule must never make the output longer.
func (c *compiledRule) groupBy(lines []string) []string {
	if c.group == nil {
		return lines
	}
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		first := c.groupParts(lines[i])
		if !first.matched {
			out = append(out, lines[i])
			i++
			continue
		}
		j := i + 1
		for j < len(lines) {
			if next := c.groupParts(lines[j]); !next.matched || next.key != first.key {
				break
			}
			j++
		}
		if j-i == 1 {
			out = append(out, lines[i])
			i = j
			continue
		}
		out = append(out, first.header)
		for ; i < j; i++ {
			out = append(out, groupIndent+c.groupParts(lines[i]).body)
		}
	}
	return out
}

// groupLine is one line as seen by group_by.
type groupLine struct {
	matched     bool
	key, header string
	body        string
}

// groupParts splits a line by the group_by regexp: key is the captured group
// the neighbouring lines must share, header the whole match (what a group
// prints once) and body what follows it. A line that is nothing but the key
// keeps itself as its body.
func (c *compiledRule) groupParts(line string) groupLine {
	m := c.group.FindStringSubmatchIndex(line)
	if m == nil {
		return groupLine{}
	}
	key := ""
	if start, end := m[2*c.groupKey], m[2*c.groupKey+1]; start >= 0 && end > start {
		key = line[start:end]
	}
	body := line[m[1]:]
	if body == "" {
		body = line
	}
	return groupLine{matched: true, key: key, header: line[m[0]:m[1]], body: body}
}

// headTail keeps the first head and the last tail lines of the rule's result,
// with a marker line where the rest went (0 = that end is not limited).
func (c *compiledRule) headTail(lines []string) []string {
	if c.head <= 0 && c.tail <= 0 || len(lines) <= c.head+c.tail {
		return lines
	}
	out := make([]string, 0, c.head+c.tail+1)
	out = append(out, lines[:c.head]...)
	out = append(out, fmt.Sprintf("[... %d lines omitted ...]", len(lines)-c.head-c.tail))
	out = append(out, lines[len(lines)-c.tail:]...)
	return out
}

// foldPlaceholder matches a {name} or {1} placeholder in a fold block's text.
var foldPlaceholder = regexp.MustCompile(`\{([^{}]*)\}`)

// renderFoldText fills a fold block's text: {lines} is how many lines the block
// swallowed and {1}/{name} are the capture groups of its start line (m is that
// line's match), so the folded line can name what it folded. Anything else in
// braces is kept as written.
func renderFoldText(f compiledFold, lines int, m []string) string {
	return foldPlaceholder.ReplaceAllStringFunc(f.text, func(ph string) string {
		key := ph[1 : len(ph)-1]
		if key == "lines" {
			return strconv.Itoa(lines)
		}
		if i, err := strconv.Atoi(key); err == nil {
			if i > 0 && i < len(m) {
				return m[i]
			}
			return ""
		}
		for i, name := range f.names {
			if name != "" && name == key && i < len(m) {
				return m[i]
			}
		}
		return ph
	})
}
