//go:build !windows

package utils

import "os"

func ReplaceFile(src, dst string) error {
	return os.Rename(src, dst)
}
