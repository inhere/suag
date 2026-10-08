//go:build windows

package native

import (
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestResolveSandboxedPath_WindowsDrive(t *testing.T) {
	ws := `C:\ws`
	want := filepath.Join(ws, "a.txt")

	for _, p := range []string{`C:\ws\a.txt`, `C:/ws/a.txt`, `c:\WS\a.txt`, `C:\ws\x\..\a.txt`} {
		t.Run("inside "+p, func(t *testing.T) {
			got, err := ResolveSandboxedPath(ws, p)
			assert.NoErr(t, err)
			// The workspace prefix is normalized to the workspace's own spelling.
			assert.Eq(t, want, got)
		})
	}
	for _, p := range []string{`C:\ws2\a.txt`, `D:\ws\a.txt`, `C:\a.txt`, `C:a.txt`, `\\host\share\ws\a.txt`} {
		t.Run("outside "+p, func(t *testing.T) {
			_, err := ResolveSandboxedPath(ws, p)
			assert.Err(t, err)
			assert.StrContains(t, err.Error(), "path escape")
		})
	}
}
