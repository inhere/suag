package native

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// ignoredDirs are never descended into by default: they are large and rarely
// what the model is looking for. no_ignore lifts this, except for .git.
var ignoredDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, "dist": true}

// maxNotedDirs is how many skipped directories the ignore note names.
const maxNotedDirs = 5

// ignoreRule is one .gitignore line, already split into path segments.
type ignoreRule struct {
	segs    []string // slash-separated pattern; non-anchored patterns start with "**"
	negate  bool     // "!pattern": re-include
	dirOnly bool     // "pattern/": directories only
}

// ignoreMatcher applies the .gitignore files of a tree the way git does for
// the common cases: every directory's .gitignore (read lazily through root)
// applies below it, the last matching rule wins, "!" re-includes, "**" spans
// directories, a "/" at the start or in the middle anchors a pattern to its
// .gitignore's directory, and a trailing "/" matches directories only. With
// all set nothing is ignored except .git. It also records what it skipped so
// tools can say so.
type ignoreMatcher struct {
	root  *os.Root
	all   bool                    // no_ignore: only .git is skipped
	rules map[string][]ignoreRule // by directory ("." = root)

	skippedDirs  []string // first maxNotedDirs skipped directories
	dirCount     int
	skippedFiles int
}

func newIgnoreMatcher(root *os.Root, noIgnore bool) *ignoreMatcher {
	return &ignoreMatcher{root: root, all: noIgnore, rules: map[string][]ignoreRule{}}
}

// skipDir reports whether the directory rel (slash-separated, relative to the
// root) is not entered, and records it when it is.
func (m *ignoreMatcher) skipDir(rel, name string) bool {
	if name == ".git" {
		return true // never searched and not worth a note
	}
	if m.all || !(ignoredDirs[name] || m.ignored(rel, true)) {
		return false
	}
	m.dirCount++
	if len(m.skippedDirs) < maxNotedDirs {
		m.skippedDirs = append(m.skippedDirs, rel+"/")
	}
	return true
}

// skipFile reports whether the file rel is ignored, and counts it when it is.
func (m *ignoreMatcher) skipFile(rel string) bool {
	if m.all || !m.ignored(rel, false) {
		return false
	}
	m.skippedFiles++
	return true
}

// note says what was skipped and how to include it; empty when nothing was.
func (m *ignoreMatcher) note() string {
	if m.dirCount == 0 && m.skippedFiles == 0 {
		return ""
	}
	var parts []string
	if m.dirCount > 0 {
		dirs := strings.Join(m.skippedDirs, ", ")
		if more := m.dirCount - len(m.skippedDirs); more > 0 {
			dirs += fmt.Sprintf(" and %d more", more)
		}
		parts = append(parts, fmt.Sprintf("%d ignored dir(s): %s", m.dirCount, dirs))
	}
	if m.skippedFiles > 0 {
		parts = append(parts, fmt.Sprintf("%d ignored file(s)", m.skippedFiles))
	}
	return "[skipped " + strings.Join(parts, "; ") +
		" (.gitignore and node_modules/vendor/dist); pass no_ignore: true to include them]"
}

// ignored applies the rules of every .gitignore from the root down to rel's
// parent; the last matching rule decides.
func (m *ignoreMatcher) ignored(rel string, isDir bool) bool {
	segs := strings.Split(rel, "/")
	ignored := false
	for depth := 0; depth < len(segs); depth++ {
		dir := "."
		if depth > 0 {
			dir = strings.Join(segs[:depth], "/")
		}
		for _, r := range m.rulesFor(dir) {
			if r.dirOnly && !isDir {
				continue
			}
			if matchSegments(r.segs, segs[depth:]) {
				ignored = !r.negate
			}
		}
	}
	return ignored
}

// rulesFor returns the parsed <dir>/.gitignore, reading it on first use.
func (m *ignoreMatcher) rulesFor(dir string) []ignoreRule {
	if rules, ok := m.rules[dir]; ok {
		return rules
	}
	var rules []ignoreRule
	if f, err := m.root.Open(path.Join(dir, ".gitignore")); err == nil {
		rules = parseGitignore(f)
		f.Close()
	}
	m.rules[dir] = rules
	return rules
}

// parseGitignore reads .gitignore lines (at most 256 KiB).
func parseGitignore(r io.Reader) []ignoreRule {
	var rules []ignoreRule
	sc := bufio.NewScanner(io.LimitReader(r, 256<<10))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rule := ignoreRule{}
		if strings.HasPrefix(line, "!") {
			rule.negate = true
			line = line[1:]
		} else if strings.HasPrefix(line, `\`) { // "\#", "\!": literal first character
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			rule.dirOnly = true
			line = strings.TrimRight(line, "/")
		}
		anchored := strings.Contains(line, "/")
		line = strings.TrimPrefix(line, "/")
		if line == "" {
			continue
		}
		rule.segs = strings.Split(line, "/")
		if !anchored {
			rule.segs = append([]string{"**"}, rule.segs...)
		}
		rules = append(rules, rule)
	}
	return rules
}
