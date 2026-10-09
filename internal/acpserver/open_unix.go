//go:build unix

package acpserver

import (
	"os"
	"syscall"
)

// openPreview opens rel read-only through root. O_NONBLOCK keeps opening a
// FIFO from blocking until a writer shows up; the caller still rejects
// non-regular files after Stat on the handle.
func openPreview(root *os.Root, rel string) (*os.File, error) {
	return root.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
