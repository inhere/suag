//go:build windows

package native

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// CanonicalPath returns path spelled the way Windows reports the location, so
// two spellings of one directory compare equal. A CI runner's %TEMP% reads with
// an 8.3 short name ("C:\Users\RUNNER~1\AppData\Local\Temp") while the same
// location has a long one ("C:\Users\runneradmin\..."), and a directory is
// recorded in one spelling and looked up in the other (an authorized directory
// is recorded as the resolved real path, the tool's path argument comes in as
// the model spelled it).
//
// Short names are expanded with GetLongPathName; the part of a path that does
// not exist yet (a file about to be created) is kept as given, and any other
// failure keeps path as it is, so a comparison only stays lexical then. Every
// "is this path inside that root" check resolves both sides through this one
// function (see absUnderWorkspace and internal/adddir's duplicate check).
func CanonicalPath(path string) string {
	path = filepath.Clean(path)
	// A short name always holds a "~" ("RUNNER~1"); without one the location
	// has no short spelling to look up and needs no syscall.
	if !filepath.IsAbs(path) || !strings.Contains(path, "~") {
		return path
	}
	// Only existing names have a long spelling, so resolve the longest existing
	// prefix as a whole and keep the missing tail unchanged.
	existing, tail := path, []string(nil)
	for {
		long, err := longPathName(existing)
		if err == nil {
			return filepath.Join(append([]string{long}, tail...)...)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return path
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return path
		}
		tail = append([]string{filepath.Base(existing)}, tail...)
		existing = parent
	}
}

// longPathName is GetLongPathName with the buffer grown to the size the API
// asks for: it reports the needed length (terminator included) when the buffer
// was too small, which is no error.
func longPathName(path string) (string, error) {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	buf := make([]uint16, windows.MAX_PATH)
	for {
		n, err := windows.GetLongPathName(ptr, &buf[0], uint32(len(buf)))
		if err != nil {
			return "", err
		}
		if int(n) < len(buf) {
			return windows.UTF16ToString(buf[:n]), nil
		}
		buf = make([]uint16, n+1)
	}
}
