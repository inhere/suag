//go:build !windows

package wiring

import (
	"os"

	"golang.org/x/sys/unix"
)

type approvalRuleLock struct {
	file *os.File
}

func acquireApprovalRuleLock(path string) (*approvalRuleLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &approvalRuleLock{file: file}, nil
}

func (l *approvalRuleLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	closeErr := l.file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
