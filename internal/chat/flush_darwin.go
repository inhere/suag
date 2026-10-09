//go:build darwin

package chat

import "golang.org/x/sys/unix"

// fread is FREAD from <sys/fcntl.h>: TIOCFLUSH with it flushes the input queue.
const fread = 0x1

// flushTerminalInput discards terminal input received but not read yet
// (tcflush(fd, TCIFLUSH) is TIOCFLUSH with FREAD on darwin).
func flushTerminalInput(fd int) error {
	return unix.IoctlSetPointerInt(fd, unix.TIOCFLUSH, fread)
}
