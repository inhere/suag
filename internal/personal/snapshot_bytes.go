package personal

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// SnapshotDirectoryBytes returns the on-disk size of one snapshots directory.
// WalkDir does not follow links, so GC reporting cannot be redirected outside
// the session root by a malformed snapshot entry.
func SnapshotDirectoryBytes(dir string) (int64, error) {
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return 0, fmt.Errorf("snapshots: %s is not a directory", dir)
	}
	var total int64
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Type()&os.ModeSymlink != 0 {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		fileInfo, err := entry.Info()
		if err != nil {
			return err
		}
		total += fileInfo.Size()
		return nil
	})
	return total, err
}
