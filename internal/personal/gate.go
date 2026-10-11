package personal

import (
	"sync"

	"github.com/inhere/suag/internal/personal/process"
)

// HoldSessionLock reserves a session for the lifetime of an interactive chat.
func (r *Runner) HoldSessionLock(sessionID string) (func() error, error) {
	if r.cfg.LockRoot == "" {
		return func() error { return nil }, nil
	}
	r.locksMu.Lock()
	if _, exists := r.heldLocks[sessionID]; exists {
		r.locksMu.Unlock()
		return func() error { return nil }, nil
	}
	r.locksMu.Unlock()
	lock, err := process.AcquireSessionLock(r.cfg.LockRoot, sessionID, 0)
	if err != nil {
		return nil, err
	}
	r.locksMu.Lock()
	if existing := r.heldLocks[sessionID]; existing != nil {
		r.locksMu.Unlock()
		_ = lock.Close()
		return func() error { return nil }, nil
	}
	r.heldLocks[sessionID] = lock
	r.locksMu.Unlock()
	var once sync.Once
	return func() error {
		var closeErr error
		once.Do(func() {
			r.locksMu.Lock()
			delete(r.heldLocks, sessionID)
			r.locksMu.Unlock()
			closeErr = lock.Close()
		})
		return closeErr
	}, nil
}
