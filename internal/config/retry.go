package axconfig

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	gkconfig "github.com/gookit/config/v2"

	"github.com/inhere/suag/pkg/agentcore/llm"
)

// ReadRetryPolicy reads retry: (how every provider retries a model request
// that failed for a transient reason, see llm.RetryPolicy): max_attempts
// (>= 1, the first attempt included; 1 turns retries off), and the
// durations base_delay, max_delay and reset_after ("2s", "1m", or seconds).
// Unset keys take llm.DefaultRetryPolicy; an invalid value is an error.
func ReadRetryPolicy() (llm.RetryPolicy, error) {
	out := llm.DefaultRetryPolicy
	if gkconfig.Get("retry.max_attempts") != nil {
		raw := strings.TrimSpace(gkconfig.String("retry.max_attempts"))
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return llm.RetryPolicy{}, fmt.Errorf("retry.max_attempts must be a whole number >= 1, got %q", raw)
		}
		out.MaxAttempts = n
	}
	for key, field := range map[string]*time.Duration{
		"base_delay": &out.BaseDelay, "max_delay": &out.MaxDelay, "reset_after": &out.ResetAfter,
	} {
		if gkconfig.Get("retry."+key) == nil {
			continue
		}
		d, err := parseIdleDuration(strings.TrimSpace(gkconfig.String("retry." + key)))
		if err != nil || d == 0 {
			if err == nil {
				err = fmt.Errorf("must be above 0")
			}
			return llm.RetryPolicy{}, fmt.Errorf("retry.%s: %w", key, err)
		}
		*field = d
	}
	if out.MaxDelay < out.BaseDelay {
		return llm.RetryPolicy{}, fmt.Errorf("retry.max_delay (%s) is below retry.base_delay (%s)", out.MaxDelay, out.BaseDelay)
	}
	return out, nil
}
