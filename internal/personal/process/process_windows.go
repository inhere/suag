//go:build windows

package process

import (
	"errors"
	"fmt"
	"math"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

const processStillActive = 259

func processExists(pid int) bool {
	if pid <= 0 || pid > math.MaxUint32 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// Only ERROR_INVALID_PARAMETER proves that the PID does not exist.
		return !processErrorMeansDead(err)
	}
	var exitCode uint32
	queryErr := windows.GetExitCodeProcess(handle, &exitCode)
	_ = windows.CloseHandle(handle)
	if queryErr != nil {
		return true
	}
	return exitCode == processStillActive
}

func processErrorMeansDead(err error) bool {
	return errors.Is(err, windows.ERROR_INVALID_PARAMETER)
}

func validateSocketPath(path string) error {
	if len(utf16.Encode([]rune(path))) > 100 {
		return fmt.Errorf("local socket path exceeds Windows AF_UNIX limit (100 UTF-16 code units): %s", path)
	}
	return nil
}
