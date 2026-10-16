package axconfig

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ResolveMaxOutputTokens returns how many tokens one main-loop request may
// answer with. Precedence is the same as for the step limit:
// --max-output-tokens > SUGA_MAX_OUTPUT_TOKENS >
// providers.<id>.models.<m>.max_output_tokens >
// providers.<id>.max_output_tokens; flagValue 0 means "not given".
//
// 0 means no configured limit: the protocol's own default applies then (the
// Claude provider sends its 32000 default, an OpenAI-compatible server uses
// its own). A value below 1 from the flag or the environment is an error
// rather than a silent "no limit", like ReadMaxSteps.
func ResolveMaxOutputTokens(flagValue int, spec ModelSpec) (int, error) {
	if flagValue < 0 {
		return 0, fmt.Errorf("--max-output-tokens must be a whole number >= 1, got %d", flagValue)
	}
	if flagValue > 0 {
		return flagValue, nil
	}
	if raw := strings.TrimSpace(os.Getenv("SUGA_MAX_OUTPUT_TOKENS")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return 0, fmt.Errorf("SUGA_MAX_OUTPUT_TOKENS must be a whole number >= 1, got %q", raw)
		}
		return n, nil
	}
	return MaxOutputTokensFor(spec), nil
}
