//go:build windows

package native

import (
	"os"

	"golang.org/x/sys/windows"
)

// IsHardLinked reports whether a regular file has another directory entry.
// Undo refuses such targets because writing one link would mutate the other.
func IsHardLinked(path string, info os.FileInfo) bool {
	if !info.Mode().IsRegular() {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return true
	}
	defer file.Close()
	var data windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &data); err != nil {
		return true
	}
	return data.NumberOfLinks > 1
}
