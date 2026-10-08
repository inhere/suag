//go:build windows

package process

import (
	"math"
	"strconv"

	"golang.org/x/sys/windows"
)

// processStartID returns the creation time of pid, which tells a reused PID apart
// from the process that wrote a record. Empty means unknown.
func processStartID(pid int) string {
	if pid <= 0 || pid > math.MaxUint32 {
		return ""
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(handle)
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return ""
	}
	return strconv.FormatInt(creation.Nanoseconds(), 10)
}
