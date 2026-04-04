//go:build !windows

package files

import "os"

func replaceFile(src, dst string) error {
	return os.Rename(src, dst)
}
