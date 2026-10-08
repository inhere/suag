//go:build !windows

package process

import (
	"errors"
	"os"
	"syscall"
)

func processExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || !errors.Is(err, syscall.ESRCH)
}

func validateSocketPath(string) error { return nil }
