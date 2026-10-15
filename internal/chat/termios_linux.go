//go:build linux

package chat

import "golang.org/x/sys/unix"

// getTermiosRequest and setTermiosRequest are tcgetattr/tcsetattr on Linux.
const (
	getTermiosRequest = unix.TCGETS
	setTermiosRequest = unix.TCSETS
)
