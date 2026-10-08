//go:build !windows

package process

import (
	"errors"
	"math"
	"syscall"
)

func processExists(pid int) bool {
	// pid_t is 32-bit: larger values would wrap (MaxInt64 becomes -1, i.e. every process).
	if pid <= 0 || pid > math.MaxInt32 {
		return false
	}
	// Signal the PID directly: os.FindProcess uses a pidfd on Linux and reports an
	// exited process as os.ErrProcessDone rather than ESRCH.
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func validateSocketPath(string) error { return nil }
