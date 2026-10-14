//go:build windows

package chat

import (
	"time"

	"golang.org/x/sys/windows"
)

// waitInputSupported reports that waitTerminalInput really asks the platform
// whether a byte is ready: the decisions built on it (a lone Esc, a pasted
// newline) may then trust an answer of "nothing ready".
const waitInputSupported = true

// waitTerminalInput reports whether the console input has a byte ready within
// timeout, without reading it (WaitForSingleObject on the console input handle,
// which stays signalled while input waits to be read). Whether a Ctrl+C pressed
// while the console waits comes back as the byte 0x03 is not verified here; see
// scripts/wincons.
func waitTerminalInput(fd int, timeout time.Duration) bool {
	event, err := windows.WaitForSingleObject(windows.Handle(fd), uint32(timeout/time.Millisecond))
	return err == nil && event == windows.WAIT_OBJECT_0
}
