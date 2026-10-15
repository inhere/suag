//go:build linux || darwin

package chat

import "golang.org/x/sys/unix"

// restoreOutputProcessing turns OPOST|ONLCR back on after the tty was put into
// raw mode. Raw input (which chat.steering keeps while a turn runs) clears the
// output flags too, and chat's own output then writes "\n" without the carriage
// return: every line of a tool result, a notice or the model's answer would start
// where the previous one ended (a staircase down the screen). The line editor is
// unaffected - it writes "\r\n" itself.
//
// The request codes differ per system (see termios_*.go); a failure is cosmetic
// (output only), so callers may ignore the error.
func restoreOutputProcessing(fd int) error {
	termios, err := unix.IoctlGetTermios(fd, getTermiosRequest)
	if err != nil {
		return err
	}
	termios.Oflag |= unix.OPOST | unix.ONLCR
	return unix.IoctlSetTermios(fd, setTermiosRequest, termios)
}
