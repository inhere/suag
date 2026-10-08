package process

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
)

func TestSessionLockSingleOwnerUnderContention(t *testing.T) {
	lockRoot := filepath.Join(shortProcessRoot(t), "locks")
	var holders, maxHolders atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				lock, err := AcquireSessionLock(lockRoot, "contended", 0)
				if errors.Is(err, ErrSessionLocked) {
					continue
				}
				if err != nil {
					t.Error(err)
					return
				}
				now := holders.Add(1)
				for {
					prev := maxHolders.Load()
					if now <= prev || maxHolders.CompareAndSwap(prev, now) {
						break
					}
				}
				time.Sleep(time.Millisecond)
				holders.Add(-1)
				if err := lock.Close(); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	assert.Eq(t, int32(1), maxHolders.Load())
}

func TestSessionLockReleasedWhenOwnerIsKilled(t *testing.T) {
	root := shortProcessRoot(t)
	lockRoot := filepath.Join(root, "locks")
	ready := filepath.Join(root, "lock.ready")
	cmd := exec.Command(os.Args[0], "-test.run=TestLockChildFixture")
	cmd.Env = append(os.Environ(), "SUAG_LOCK_FIXTURE=1", "SUAG_LOCK_ROOT="+lockRoot, "SUAG_LOCK_READY="+ready)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not take the lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, err := AcquireSessionLock(lockRoot, "crashed", 0)
	assert.True(t, errors.Is(err, ErrSessionLocked))

	// A hard kill skips Close: the OS must still release the lock.
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	lock, err := AcquireSessionLock(lockRoot, "crashed", 2*time.Second)
	assert.NoErr(t, err)
	assert.NoErr(t, lock.Close())
}

func TestLockChildFixture(t *testing.T) {
	if os.Getenv("SUAG_LOCK_FIXTURE") != "1" {
		return
	}
	if _, err := AcquireSessionLock(os.Getenv("SUAG_LOCK_ROOT"), "crashed", 0); err != nil {
		os.Exit(10)
	}
	_ = os.WriteFile(os.Getenv("SUAG_LOCK_READY"), nil, 0o600)
	for {
		time.Sleep(time.Second)
	}
}
