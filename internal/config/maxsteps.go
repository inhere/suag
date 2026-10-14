package axconfig

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	gkconfig "github.com/gookit/config/v2"
)

// DefaultMaxSteps is the per-turn step limit of the main agent when neither
// max_steps nor SUGA_MAX_STEPS is set.
const DefaultMaxSteps = 100

// ReadMaxSteps resolves the step limit of one main-agent turn. Precedence is
// the same as for the model settings: --max-steps > SUGA_MAX_STEPS > config
// max_steps (default 100); flagMaxSteps 0 means "not given". A value below 1
// from the flag or the environment is an error rather than a silent "no limit";
// a non-positive max_steps in the config keeps its older meaning (no limit).
// The sub-agent limit agents.max_steps is a separate setting (ReadAgentSettings).
func ReadMaxSteps(flagMaxSteps int) (int, error) {
	if flagMaxSteps < 0 {
		return 0, fmt.Errorf("--max-steps must be a whole number >= 1, got %d", flagMaxSteps)
	}
	if flagMaxSteps > 0 {
		return flagMaxSteps, nil
	}
	if raw := strings.TrimSpace(os.Getenv("SUGA_MAX_STEPS")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return 0, fmt.Errorf("SUGA_MAX_STEPS must be a whole number >= 1, got %q", raw)
		}
		return n, nil
	}
	return gkconfig.Int("max_steps", DefaultMaxSteps), nil
}
