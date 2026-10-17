//go:build windows

package axconfig

import (
	"os"

	"golang.org/x/sys/windows"
)

func lockBootstrapFile(file *os.File) error {
	offset := &windows.Overlapped{OffsetHigh: 1}
	return windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, offset)
}

func unlockBootstrapFile(file *os.File) error {
	offset := &windows.Overlapped{OffsetHigh: 1}
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, offset)
}

// MoveFileEx replaces an existing destination without the truncate-then-write
// window that would expose a partial config on Windows.
func replaceBootstrapFile(source, target string) error {
	sourceName, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	targetName, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(sourceName, targetName, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func syncBootstrapDir(string) error {
	// Windows does not expose a directory handle that supports file Sync.
	return nil
}
