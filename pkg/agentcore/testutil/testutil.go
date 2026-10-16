// Package testutil holds small helpers shared by the tests of this module.
//
// Dir exists because two needs meet in the same temporary directory: a test
// that binds a unix socket needs a path short enough for sockaddr_un.sun_path
// (t.TempDir() carries the test name and is not guaranteed to be), and every
// test's scratch files belong outside the repository - a directory under
// <repo>/tmp/ is scanned by `go list ./...` while another go command may be
// deleting it (issue suga-wab3).
package testutil

import (
	"os"
	"runtime"
	"testing"
)

// socketPathLimit is the most bytes a socket path may have on any unix suga runs
// on: sockaddr_un.sun_path holds 108 bytes on Linux and 104 on darwin and the
// BSDs, and one byte of each goes to the trailing NUL (see the process package's
// socketPathLimit, which validates the real path at listen time).
const socketPathLimit = 103

// socketNameBytes is what a session socket adds inside such a directory: a
// "<pid>.sock" file, for the chat under its "run/" subdirectory.
const socketNameBytes = len("/run/999999.sock")

// Dir returns a fresh temporary directory and removes it when the test ends.
// It is created under the system temporary directory (/tmp, %TEMP%), whose path
// is short; on unix the result is checked against the socket path limit, so a
// machine with an unusually long temporary directory fails here instead of in
// an opaque bind error. Like t.TempDir it may be called several times.
func Dir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "s6-")
	if err != nil {
		t.Fatalf("short test temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if runtime.GOOS != "windows" { // a named pipe has no such limit
		if n := len(dir) + socketNameBytes; n > socketPathLimit {
			t.Fatalf("the temporary directory is too long for a session socket: %s (%d bytes used, at most %d fit): "+
				"point TMPDIR at a shorter directory", dir, n, socketPathLimit)
		}
	}
	return dir
}
