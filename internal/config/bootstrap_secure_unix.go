//go:build !windows

package axconfig

import "os"

func secureBootstrapDir(path string) error {
	// On Unix, mode 0700 both restricts the directory and makes its inherited
	// files private to the current user.
	return os.Chmod(path, 0o700)
}
