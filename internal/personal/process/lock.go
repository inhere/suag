package process

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type lockRecord struct {
	PID       int       `json:"pid"`
	Host      string    `json:"host"`
	SessionID string    `json:"session_id"`
	CreatedAt time.Time `json:"created_at"`
}

type SessionLock struct {
	path string
	file *os.File
	rec  lockRecord
}

var ErrSessionLocked = errors.New("session is locked by another Suag process")

// AcquireSessionLock holds an OS file lock (flock / LockFileEx) on <root>/<session>.lock.
// The OS releases it when the owner exits, so a crashed owner never needs PID-based
// reclaiming, and two contenders can never both win. The JSON content is diagnostic only.
func AcquireSessionLock(root, sessionID string, wait time.Duration) (*SessionLock, error) {
	return AcquireSessionLockContext(context.Background(), root, sessionID, wait)
}

// AcquireSessionLockContext is AcquireSessionLock whose wait aborts as soon as ctx is
// cancelled (Ctrl-C / SIGTERM), returning ctx.Err() instead of sleeping out the timeout.
func AcquireSessionLockContext(ctx context.Context, root, sessionID string, wait time.Duration) (*SessionLock, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	_ = os.Chmod(root, 0o700)
	name := strings.NewReplacer("/", "_", "\\", "_").Replace(sessionID)
	path := filepath.Join(root, name+".lock")
	host, _ := os.Hostname()
	record := lockRecord{PID: os.Getpid(), Host: host, SessionID: sessionID, CreatedAt: time.Now().UTC()}
	deadline := time.Now().Add(wait)
	for {
		lock, err := tryAcquire(path, record)
		if err != nil {
			return nil, err
		}
		if lock != nil {
			return lock, nil
		}
		if wait <= 0 || time.Now().After(deadline) {
			return nil, fmt.Errorf("%w: %s", ErrSessionLocked, sessionID)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// tryAcquire returns (nil, nil) when another process holds the lock.
func tryAcquire(path string, record lockRecord) (*SessionLock, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if errors.Is(err, os.ErrNotExist) {
		// Some filesystems (e.g. WSL/9p mounts) fail a create that races a releasing
		// owner's unlink; treat it as a busy lock and let the caller retry.
		if _, dirErr := os.Stat(filepath.Dir(path)); dirErr == nil {
			return nil, nil
		}
	}
	if err != nil {
		return nil, fmt.Errorf("open session lock: %w", err)
	}
	locked, err := tryLockFile(file)
	if err != nil || !locked {
		_ = file.Close()
		if err != nil {
			return nil, fmt.Errorf("lock session: %w", err)
		}
		return nil, nil
	}
	// A releasing owner unlinks the file before unlocking; if we locked that unlinked
	// file, the path now names a different file and our lock protects nothing.
	opened, statErr := file.Stat()
	current, pathErr := os.Stat(path)
	if statErr != nil || pathErr != nil || !os.SameFile(opened, current) {
		_ = unlockFile(file)
		_ = file.Close()
		return nil, nil
	}
	data, _ := json.Marshal(record)
	if err := file.Truncate(0); err == nil {
		_, _ = file.WriteAt(data, 0)
	}
	return &SessionLock{path: path, file: file, rec: record}, nil
}

func (l *SessionLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	// Unlink while still holding the lock (see tryAcquire). Windows may refuse to delete
	// an open file; the file then stays and the next owner simply locks it again.
	_ = os.Remove(l.path)
	unlockErr := unlockFile(l.file)
	closeErr := l.file.Close()
	l.file = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
