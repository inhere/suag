//go:build !windows

package process

import (
	"math"
	"os"
	"os/exec"
	"testing"
)

func TestProcessExistsUnix(t *testing.T) {
	if !processExists(os.Getpid()) {
		t.Fatal("current process should exist")
	}
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skipf("cannot run true: %v", err)
	}
	if processExists(cmd.Process.Pid) {
		t.Fatalf("exited child %d should not exist", cmd.Process.Pid)
	}
	for _, pid := range []int{0, -1, math.MaxInt32 + 1, math.MaxInt64} {
		if processExists(pid) {
			t.Fatalf("invalid pid %d should not exist", pid)
		}
	}
}
