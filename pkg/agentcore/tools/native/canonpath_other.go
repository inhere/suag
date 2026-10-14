//go:build !windows

package native

import "path/filepath"

// CanonicalPath returns path in the one spelling a location has on this system:
// only Windows spells one directory two ways (8.3 short names), so nothing is
// looked up and resolved here and the Unix behaviour is unchanged. It cleans
// like the Windows version, so no caller has to know which system it runs on.
func CanonicalPath(path string) string { return filepath.Clean(path) }
