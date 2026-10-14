//go:build windows

package wiring

import (
	"errors"

	"golang.org/x/sys/windows"
)

// renameConflict reports whether err is Windows refusing to rename a directory
// another process is busy with: a file inside it being read is
// ERROR_ACCESS_DENIED, the directory itself being held open is
// ERROR_SHARING_VIOLATION. Both are momentary - a reader closes its handle at
// once - so the release waits and retries instead of giving up.
func renameConflict(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
