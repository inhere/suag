package axconfig

import (
	"fmt"
	"strings"

	gkconfig "github.com/gookit/config/v2"
)

// DefaultRuleFileName is the project rule file read at every directory level
// when rules.file is unset.
const DefaultRuleFileName = "AGENTS.md"

// ReadRuleFileName reads rules.file: the project rule file read at each level,
// from the Git root (or cwd) down to cwd. Unset (or empty) means AGENTS.md.
// Only a bare file name is accepted - a path would let the value read files
// outside the level directory - so any other value is an error, not a fallback.
func ReadRuleFileName() (string, error) {
	raw := gkconfig.Get("rules.file")
	if raw == nil {
		return DefaultRuleFileName, nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("rules.file must be a file name, got %v", raw)
	}
	name := strings.TrimSpace(value)
	if name == "" {
		return DefaultRuleFileName, nil
	}
	if name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("rules.file must be a file name without a path separator, got %q", value)
	}
	return name, nil
}
