//go:build linux || darwin

package chat

import (
	"time"

	"golang.org/x/sys/unix"
)

// waitTerminalInput reports whether the terminal has a byte ready within timeout,
// without reading it (poll(2)): the wait behind the lone-Esc decision, which must
// not leave a group+blocked read behind. A poll that fails (a closed terminal, no
// such fd) reports nothing ready, which keeps the Esc byte in the editor as the
// start of a sequence.
func waitTerminalInput(fd int, timeout time.Duration) bool {
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	ms := int(timeout / time.Millisecond)
	n, err := unix.Poll(fds, ms)
	return err == nil && n > 0
}
