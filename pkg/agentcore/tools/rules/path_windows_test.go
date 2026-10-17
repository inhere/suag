//go:build windows

package rules

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"golang.org/x/sys/windows"
)

func windowsShortPath(t *testing.T, value string) string {
	t.Helper()
	ptr, err := windows.UTF16PtrFromString(value)
	assert.Require(t, assert.NoErr(t, err))
	buf := make([]uint16, windows.MAX_PATH)
	for {
		n, err := windows.GetShortPathName(ptr, &buf[0], uint32(len(buf)))
		assert.Require(t, assert.NoErr(t, err))
		if int(n) < len(buf) {
			return windows.UTF16ToString(buf[:n])
		}
		buf = make([]uint16, n+1)
	}
}

func TestRuleMatchesPathWindowsNames(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	assert.NoErr(t, os.MkdirAll(bin, 0o755))
	assert.NoErr(t, os.WriteFile(filepath.Join(bin, "Case.EXE"), []byte("x"), 0o644))
	allow := Rule{Tool: "file_write", Root: root, Path: []string{"bin/**"}, Action: Allow}

	t.Run("case insensitive and exe suffix are ordinary path matching", func(t *testing.T) {
		assert.True(t, allow.MatchesPath(Target{Tool: "file_write", Root: root, Path: `bin\case.exe`}))
		assert.False(t, (Rule{Tool: "file_write", Root: root, Path: []string{"bin/tool"}, Action: Allow}).MatchesPath(Target{Tool: "file_write", Root: root, Path: `bin\Case.EXE`}))
	})

	t.Run("alternate data stream does not allow", func(t *testing.T) {
		assert.False(t, allow.MatchesPath(Target{Tool: "file_write", Root: root, Path: `bin\Case.EXE:stream`}))
	})

	t.Run("trailing dot and space do not allow", func(t *testing.T) {
		assert.False(t, allow.MatchesPath(Target{Tool: "file_write", Root: root, Path: `bin\new. `}))
		assert.False(t, allow.MatchesPath(Target{Tool: "file_write", Root: root, Path: `bin\new.`}))
	})

	t.Run("short name does not allow", func(t *testing.T) {
		short := windowsShortPath(t, bin)
		if short == bin {
			t.Skip("8.3 short names are not enabled on this volume")
		}
		target := filepath.Join(short, "Case.EXE")
		assert.False(t, allow.MatchesPath(Target{Tool: "file_write", Root: root, Path: target}))
	})
}

// TestRuleMatchesPathRootWithShortName: a short-name spelling in the approved
// root (CI runners: C:\Users\RUNNER~1\...) must not void allows below it; only
// the part below the root is checked for unsafe Windows names.
func TestRuleMatchesPathRootWithShortName(t *testing.T) {
	root := filepath.Join(t.TempDir(), "RUNNER~1")
	assert.NoErr(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	allow := Rule{Tool: "file_write", Root: root, Path: []string{"docs/**"}, Action: Allow}
	t.Run("allow below a short-name root", func(t *testing.T) {
		assert.True(t, allow.MatchesPath(Target{Tool: "file_write", Root: root, Path: `docs\a.txt`}))
	})
	t.Run("a short name below the root still gives no allow", func(t *testing.T) {
		assert.NoErr(t, os.MkdirAll(filepath.Join(root, "docs", "LONGNA~1"), 0o755))
		assert.False(t, allow.MatchesPath(Target{Tool: "file_write", Root: root, Path: `docs\LONGNA~1\a.txt`}))
	})
}
