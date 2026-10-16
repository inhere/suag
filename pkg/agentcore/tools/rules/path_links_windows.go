//go:build windows

package rules

import (
	"os"

	"golang.org/x/sys/windows"
)

func hardLinked(path string, info os.FileInfo) bool {
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
