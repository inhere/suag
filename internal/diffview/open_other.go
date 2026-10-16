//go:build !unix

package diffview

import "os"

// openPreview opens rel read-only through root. There is no O_NONBLOCK here;
// the caller rejects non-regular files after Stat on the handle.
func openPreview(root *os.Root, rel string) (*os.File, error) {
	return root.OpenFile(rel, os.O_RDONLY, 0)
}
