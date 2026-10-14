//go:build windows

package chat

import (
	"io"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The pinned golang.org/x/sys does not wrap SetConsoleTitleW, so it is called
// from kernel32.dll directly (same as flush_windows.go).
var procSetConsoleTitle = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetConsoleTitleW")

// nativeTitleSupport reports how this host reacts to the OSC 0 sequence and how
// to set the title when it does not interpret escape sequences.
//
// A Windows console only interprets them with ENABLE_VIRTUAL_TERMINAL_PROCESSING
// set; the color package turns that mode on for the process at startup (Windows
// 10 build 14931 and later, path detect_windows.go), and a host that keeps its
// own console (Zed, Windows Terminal, VS Code) is covered by the sequence either
// way. Where the mode is off - a classic conhost, or a console the color package
// could not patch - writing the object would show it as plain text, so the title
// goes through the console API instead: the console title is what the window
// and the tab show there.
func nativeTitleSupport(device io.Writer) (escape bool, native func(string) error) {
	file, ok := device.(*os.File)
	if !ok {
		// No console handle to ask (a test buffer): the sequence is the only
		// route, and nothing is known that would forbid it.
		return true, nil
	}
	if vtProcessing(file) {
		return true, nil
	}
	return false, setConsoleTitle
}

// vtProcessing reports whether the console of file interprets escape sequences
// (its output mode has virtual terminal processing set).
func vtProcessing(file *os.File) bool {
	var mode uint32
	if err := windows.GetConsoleMode(windows.Handle(file.Fd()), &mode); err != nil {
		// Not a console (a pipe, a file): a vt-capable host is the usual reason,
		// and no console title can be set for it anyway.
		return true
	}
	return mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0
}

// setConsoleTitle sets the console title through the console API; an empty title
// clears it, like the empty OSC 0 sequence.
func setConsoleTitle(title string) error {
	ptr, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return err
	}
	if ok, _, err := procSetConsoleTitle.Call(uintptr(unsafe.Pointer(ptr))); ok == 0 {
		return err
	}
	return nil
}
