//go:build !windows

package rules

import (
	"os"
	"syscall"
)

func hardLinked(path string, info os.FileInfo) bool {
	if !info.Mode().IsRegular() {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink > 1
}
