//go:build !windows

package native

import (
	"os"
	"syscall"
)

// IsHardLinked reports whether a regular file has another directory entry.
// Undo refuses such targets because writing one link would mutate the other.
func IsHardLinked(path string, info os.FileInfo) bool {
	if !info.Mode().IsRegular() {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink > 1
}
