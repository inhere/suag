// Package redact scrubs text before it leaves the runtime or is fed back to
// the model: Secrets masks credential-like values, Clean additionally strips
// content that can be used to smuggle instructions (invisible characters,
// HTML comments, forged system tags).
package redact

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	secretValue = regexp.MustCompile(`(?i)([a-z0-9_.-]*(?:api[_-]?key|token|secret|password|authorization)[a-z0-9_.-]*)(\s*[:=]\s*)([^\s,;]+)`)
	bearerValue = regexp.MustCompile(`(?i)(authorization\s*:\s*bearer\s+)[^\s,;]+`)
	jsonSecret  = regexp.MustCompile(`(?i)(["'][^"']*(?:api[_-]?key|token|secret|password|authorization)[^"']*["']\s*:\s*["'])([^"']*)(["'])`)

	// htmlComment matches <!-- ... --> across lines (non-greedy).
	htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)

	// forgedTagNames lists tag names that imitate harness or role markers.
	forgedTagNames = `system-reminder|system_reminder|system|assistant|user|human|instructions?|developer|tool_call|tool_result|function_calls|function_results`
	// forgedBlock matches a paired forged tag together with its content.
	forgedBlock = regexp.MustCompile(`(?is)<\s*(` + forgedTagNames + `)\b[^>]*>.*?<\s*/\s*(` + forgedTagNames + `)\s*>`)
	// forgedTag matches a lone opening or closing forged tag.
	forgedTag = regexp.MustCompile(`(?i)<\s*/?\s*(` + forgedTagNames + `)\b[^>]*>`)
)

// Secrets removes credential-like values before tool data leaves the runtime.
func Secrets(value string) string {
	value = jsonSecret.ReplaceAllString(value, "$1[REDACTED]$3")
	value = bearerValue.ReplaceAllString(value, "$1[REDACTED]")
	return secretValue.ReplaceAllStringFunc(value, func(match string) string {
		groups := secretValue.FindStringSubmatch(match)
		// "Authorization: Bearer x" was already handled by bearerValue; keep its scheme word.
		if len(groups) == 4 && strings.EqualFold(groups[1], "authorization") && strings.EqualFold(groups[3], "Bearer") {
			return match
		}
		return groups[1] + "=[REDACTED]"
	})
}

// Clean prepares untrusted text for reuse as model context: it drops
// zero-width/invisible format characters, HTML comments and forged tags such
// as <system-reminder>...</system-reminder>, then applies Secrets. Order
// matters: invisible characters are removed first so they cannot split a tag
// or comment marker to dodge the later patterns.
func Clean(value string) string {
	value = strings.Map(func(r rune) rune {
		// Cf covers zero-width space/joiners, bidi controls, BOM and tag characters.
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, value)
	value = htmlComment.ReplaceAllString(value, "")
	value = forgedBlock.ReplaceAllString(value, "")
	value = forgedTag.ReplaceAllString(value, "")
	return Secrets(value)
}
