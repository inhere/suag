//go:build linux

package process

import (
	"os"
	"strconv"
	"strings"
)

// processStartID returns the kernel start time of pid (field 22 of /proc/<pid>/stat),
// which tells a reused PID apart from the process that wrote a record. Empty means unknown.
func processStartID(pid int) string {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ""
	}
	// comm may contain spaces and parentheses, so the fields start after the last ')'.
	text := string(data)
	end := strings.LastIndexByte(text, ')')
	if end < 0 {
		return ""
	}
	fields := strings.Fields(text[end+1:])
	// fields[0] is field 3 (state), so field 22 (starttime) is fields[19].
	if len(fields) < 20 {
		return ""
	}
	return fields[19]
}
