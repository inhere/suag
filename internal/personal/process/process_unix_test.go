//go:build !windows

package process

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
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

// TestValidateSocketPathUnix pins the AF_UNIX path limit: sockaddr_un.sun_path holds 108 bytes
// on Linux and 104 on darwin/BSD, one of which goes to the trailing NUL.
func TestValidateSocketPathUnix(t *testing.T) {
	limit := 107
	if runtime.GOOS != "linux" {
		limit = 103
	}
	t.Run("length at the limit is accepted", func(t *testing.T) {
		path := "/" + strings.Repeat("d", limit-1)
		assert.Eq(t, limit, len(path))
		assert.NoErr(t, validateSocketPath(path))
	})
	t.Run("length over the limit is rejected", func(t *testing.T) {
		path := "/" + strings.Repeat("d", limit)
		assert.Eq(t, limit+1, len(path))
		err := validateSocketPath(path)
		// The message names the limit, the path and the way out: chat prints it to the user.
		assert.Err(t, err)
		assert.ErrSubMsg(t, err, strconv.Itoa(limit))
		assert.ErrSubMsg(t, err, "SUGA_HOME")
	})
}

// TestStartRejectsOverlongSocketPathUnix checks the registration path, not only the helper:
// an overlong root must fail with the length error and leave no record or socket behind.
func TestStartRejectsOverlongSocketPathUnix(t *testing.T) {
	root := filepath.Join(t.TempDir(), strings.Repeat("d", 120))
	_, err := Start(root, "sess-long-path", "long", t.TempDir(), func(string, string, string) (Delivery, error) { return Delivery{}, nil })
	assert.ErrSubMsg(t, err, "too long")
	entries, dirErr := os.ReadDir(root)
	assert.Require(t, assert.NoErr(t, dirErr))
	assert.Len(t, entries, 0)
}
