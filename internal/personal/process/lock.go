package process

import (
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
	rec  lockRecord
}

var ErrSessionLocked = errors.New("session is locked by another Suag process")

// AcquireSessionLock uses exclusive file creation, then reclaims only a lock whose owner is provably dead.
func AcquireSessionLock(root, sessionID string, wait time.Duration) (*SessionLock, error) {
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
		data, _ := json.Marshal(record)
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			if _, writeErr := file.Write(data); writeErr != nil {
				_ = file.Close()
				_ = os.Remove(path)
				return nil, writeErr
			}
			if closeErr := file.Close(); closeErr != nil {
				_ = os.Remove(path)
				return nil, closeErr
			}
			return &SessionLock{path: path, rec: record}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create session lock: %w", err)
		}
		var existing lockRecord
		contents, readErr := os.ReadFile(path)
		if readErr == nil && json.Unmarshal(contents, &existing) == nil && existing.PID > 0 && existing.Host != "" && existing.Host == host && !processExists(existing.PID) {
			_ = os.Remove(path)
			continue
		}
		if wait <= 0 || time.Now().After(deadline) {
			return nil, fmt.Errorf("%w: %s", ErrSessionLocked, sessionID)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (l *SessionLock) Close() error {
	if l == nil {
		return nil
	}
	contents, err := os.ReadFile(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var current lockRecord
	if json.Unmarshal(contents, &current) != nil || current.PID != l.rec.PID || current.Host != l.rec.Host || current.SessionID != l.rec.SessionID {
		return nil
	}
	return os.Remove(l.path)
}
