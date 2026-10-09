//go:build linux

package chat

import "golang.org/x/sys/unix"

// flushTerminalInput discards terminal input received but not read yet
// (tcflush(fd, TCIFLUSH)).
func flushTerminalInput(fd int) error {
	return unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCIFLUSH)
}
