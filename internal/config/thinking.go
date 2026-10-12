package axconfig

import (
	"fmt"
	"slices"
	"strings"
)

// ThinkingLevels are the accepted thinking levels (--thinking,
// providers.<id>.thinking, providers.<id>.models.<key>.thinking), the same
// names omp uses. "auto" (or unset) leaves the provider's default.
var ThinkingLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max", "auto"}

// ParseThinking normalizes and validates a thinking level; "" stays unset.
func ParseThinking(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" || slices.Contains(ThinkingLevels, v) {
		return v, nil
	}
	return "", fmt.Errorf("unknown thinking level %q (want %s)", v, strings.Join(ThinkingLevels, ", "))
}

// ThinkingLevel is the effective thinking level of s: the override set from
// --thinking, then models.<key>.thinking, then providers.<id>.thinking; ""
// when none is set.
func (s ModelSpec) ThinkingLevel() string {
	if s.Thinking != "" {
		return s.Thinking
	}
	if cfg, ok := s.Provider.Models[s.Model]; ok && cfg.Thinking != "" {
		return cfg.Thinking
	}
	return s.Provider.Thinking
}

// reasoningEffort maps a thinking level to the OpenAI-compatible
// reasoning_effort field: nothing for unset/auto, otherwise the level as is.
// Providers accept different sets (command-code: off|low|medium|high|xhigh|
// max; OpenAI: none|minimal|low|medium|high), so no translation is guessed:
// an unsupported level comes back as the provider's own error.
func reasoningEffort(level string) string {
	if level == "auto" {
		return ""
	}
	return level
}
