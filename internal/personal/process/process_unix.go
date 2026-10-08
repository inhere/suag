//go:build !windows

package process

import (
	"errors"
	"math"
	"os"
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

// secureDir limits dir to the owner. Best effort, like the original chmod: a directory we
// cannot chmod (not ours) is left as is.
func secureDir(dir string) error {
	_ = os.Chmod(dir, 0o700)
	return nil
}
