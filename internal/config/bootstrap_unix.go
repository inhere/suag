//go:build !windows

package axconfig

import (
	"os"
	"syscall"
)

func lockBootstrapFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
}

func unlockBootstrapFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}

func replaceBootstrapFile(source, target string) error {
	return os.Rename(source, target)
}

func syncBootstrapDir(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
