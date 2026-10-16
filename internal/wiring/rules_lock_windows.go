//go:build windows

package wiring

import (
	"os"

	"golang.org/x/sys/windows"
)

type approvalRuleLock struct {
	file *os.File
}

func acquireApprovalRuleLock(path string) (*approvalRuleLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &windows.Overlapped{}); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &approvalRuleLock{file: file}, nil
}

func (l *approvalRuleLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, &windows.Overlapped{})
	closeErr := l.file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
