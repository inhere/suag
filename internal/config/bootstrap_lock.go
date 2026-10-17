package axconfig

import (
	"fmt"
	"os"
)

type bootstrapLock struct {
	file *os.File
}

func acquireBootstrapLock(path string) (*bootstrapLock, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockBootstrapFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("acquire %s: %w", path, err)
	}
	return &bootstrapLock{file: file}, nil
}

func (l *bootstrapLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := unlockBootstrapFile(l.file)
	closeErr := l.file.Close()
	l.file = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
