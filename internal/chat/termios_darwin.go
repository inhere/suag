//go:build darwin

package chat

import "golang.org/x/sys/unix"

// getTermiosRequest and setTermiosRequest are tcgetattr/tcsetattr on darwin
// (TIOCGETA/TIOCSETA).
const (
	getTermiosRequest = unix.TIOCGETA
	setTermiosRequest = unix.TIOCSETA
)
