package personal

import (
	"strings"
	"unicode/utf8"
)

// Sections gofer puts in front of a job's prompt (gofer internal/job): its
// rules, from a fixed header to an end marker; then the declared tools and
// the mounted skills, each a "## …（gofer 注入…）" / "## …（gofer 挂载…）"
// heading with an intro or footer line and "- " items.
const (
	goferRulesHeader = "## 必须遵守的规则（gofer 注入"
	goferRulesEnd    = "<!-- gofer:rules-end -->"
	goferSkillsTail  = "先读与本任务相关的 SKILL.md，再动手。"
)

// titleLimit caps a session title, in runes.
const titleLimit = 100

// SessionTitle derives a session title from a prompt: the first line of the
// task itself, with the sections gofer injects in front of a job's prompt
// skipped and markdown heading marks removed, at most titleLimit runes. A
// prompt that is nothing but such sections keeps its first line.
func SessionTitle(prompt string) string {
	if title := taskLine(prompt, true); title != "" {
		return title
	}
	return taskLine(prompt, false)
}

// taskLine returns the first line of prompt, skipping gofer's sections when
// skipGofer is set.
func taskLine(prompt string, skipGofer bool) string {
	rest := strings.TrimSpace(prompt)
	if !skipGofer {
		line, _, _ := strings.Cut(rest, "\n")
		return clipTitle(strings.TrimSpace(strings.TrimLeft(line, "#")))
	}
	if strings.HasPrefix(rest, goferRulesHeader) {
		if i := strings.Index(rest, goferRulesEnd); i >= 0 {
			rest = rest[i+len(goferRulesEnd):]
		}
	}
	inGofer := false // inside a tools or skills section
	for _, line := range strings.Split(rest, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "## ") && (strings.Contains(line, "（gofer 注入") || strings.Contains(line, "（gofer 挂载")):
			inGofer = true
			continue
		case inGofer && (strings.HasPrefix(line, "- ") || strings.HasSuffix(line, "：")):
			continue // a tool or skill item, or the intro line before them
		case inGofer && line == goferSkillsTail:
			inGofer = false
			continue
		}
		return clipTitle(strings.TrimSpace(strings.TrimLeft(line, "#")))
	}
	return ""
}

func clipTitle(s string) string {
	if utf8.RuneCountInString(s) <= titleLimit {
		return s
	}
	return string([]rune(s)[:titleLimit-1]) + "…"
}

// defaultTitle is the title of a session created without one (ACP).
func defaultTitle(id string) string { return "Session " + id }
