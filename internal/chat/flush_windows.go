//go:build windows

package chat

import "golang.org/x/sys/windows"

// The pinned golang.org/x/sys does not wrap FlushConsoleInputBuffer, so it is
// called from kernel32.dll directly.
var procFlushConsoleInputBuffer = windows.NewLazySystemDLL("kernel32.dll").NewProc("FlushConsoleInputBuffer")

// flushTerminalInput discards console input received but not read yet.
func flushTerminalInput(fd int) error {
	ok, _, err := procFlushConsoleInputBuffer.Call(uintptr(fd))
	if ok == 0 {
		return err
	}
	return nil
}
