package axconfig

import (
	"fmt"
	"strconv"
	"strings"

	gkconfig "github.com/gookit/config/v2"

	"github.com/inhere/suga/pkg/agentcore/tasks"
)

// TaskSettings configure background tasks (tasks.*).
type TaskSettings struct {
	Limits tasks.Limits
	// WakeOnExit: in chat, a notice left while chat waits for input (a task
	// that ended, a watch whose result changed) starts a turn with it
	// (tasks.wake_on_exit, default true).
	WakeOnExit bool
}

// ReadTaskSettings reads tasks.max_running (>= 1), tasks.max_duration (a
// duration such as "2h" or seconds), tasks.max_output_bytes (>= 1) and
// tasks.wake_on_exit. Unset keys take tasks.DefaultLimits and true.
func ReadTaskSettings() (TaskSettings, error) {
	out := TaskSettings{Limits: tasks.DefaultLimits, WakeOnExit: true}
	positive := func(key string) (int64, bool, error) {
		if gkconfig.Get(key) == nil {
			return 0, false, nil
		}
		raw := strings.TrimSpace(gkconfig.String(key))
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 1 {
			return 0, false, fmt.Errorf("%s must be a whole number >= 1, got %q", key, raw)
		}
		return n, true, nil
	}
	if n, ok, err := positive("tasks.max_running"); err != nil {
		return out, err
	} else if ok {
		out.Limits.MaxRunning = int(n)
	}
	if n, ok, err := positive("tasks.max_output_bytes"); err != nil {
		return out, err
	} else if ok {
		out.Limits.MaxOutputBytes = n
	}
	if gkconfig.Get("tasks.max_duration") != nil {
		d, err := parseIdleDuration(strings.TrimSpace(gkconfig.String("tasks.max_duration")))
		if err != nil || d == 0 {
			if err == nil {
				err = fmt.Errorf("must be above 0")
			}
			return out, fmt.Errorf("tasks.max_duration: %w", err)
		}
		out.Limits.MaxDuration = d
	}
	if raw := gkconfig.Get("tasks.wake_on_exit"); raw != nil {
		b, ok := raw.(bool)
		if !ok {
			return out, fmt.Errorf("tasks.wake_on_exit must be true or false, got %v", raw)
		}
		out.WakeOnExit = b
	}
	return out, nil
}
