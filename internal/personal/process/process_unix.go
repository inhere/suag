//go:build !windows

package process

import (
	"errors"
	"fmt"
	"math"
	"os"
	"runtime"
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

// socketPathLimit is the longest usable sockaddr_un.sun_path value on goos: the kernel (and
// Go's syscall.SockaddrUnix) needs a trailing NUL inside the fixed-size buffer, so the field
// size (108 bytes on Linux, 104 on darwin and the BSDs) leaves one byte less for the path.
func socketPathLimit(goos string) int {
	if goos == "linux" {
		return 107
	}
	return 103
}

// validateSocketPath rejects a socket path too long to bind. Without this check the failure
// surfaces as an opaque "bind: invalid argument" at listen time.
func validateSocketPath(path string) error {
	limit := socketPathLimit(runtime.GOOS)
	if len(path) > limit {
		return fmt.Errorf("local session socket path is too long: %d bytes, %s allows at most %d bytes: %s; use a shorter SUGA_HOME", len(path), runtime.GOOS, limit, path)
	}
	return nil
}

// secureDir limits dir to the owner. Best effort, like the original chmod: a directory we
// cannot chmod (not ours) is left as is.
func secureDir(dir string) error {
	_ = os.Chmod(dir, 0o700)
	return nil
}
