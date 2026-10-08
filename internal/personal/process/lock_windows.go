//go:build windows

package process

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockRegion places the byte-range lock far beyond the record content: Windows locks
// are mandatory, so locking the content itself would block readers.
func lockRegion() *windows.Overlapped {
	return &windows.Overlapped{OffsetHigh: 1}
}

// tryLockFile takes a non-blocking exclusive LockFileEx; Windows drops it when the owner exits.
func tryLockFile(file *os.File) (bool, error) {
	err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, lockRegion())
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return false, nil
	}
	return err == nil, err
}

func unlockFile(file *os.File) error {
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, lockRegion())
}
