//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// pty is a Windows pseudoconsole (ConPTY): the mechanism Windows Terminal,
// VS Code and other modern terminals use to run a console program. The program
// sees a console (its stdin/stdout are the pseudoconsole), and the terminal
// writes its input into the input pipe - for Ctrl+C that is the byte 0x03, which
// only becomes a CTRL_C_EVENT while the console is cooked.
type pty struct {
	hpc    windows.Handle
	input  *os.File // terminal -> program
	output *os.File // program -> terminal
	proc   windows.ProcessInformation
	// extra receives the output as well when a caller wants to poll it.
	extra  *lockedBuffer
	mu     sync.Mutex
	buf    []byte
	closed bool
}

// PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE from processthreadsapi.h.
const procThreadAttributePseudoConsole = 0x00020016

var (
	procCreatePseudoConsole = kernel32.NewProc("CreatePseudoConsole")
	procClosePseudoConsole  = kernel32.NewProc("ClosePseudoConsole")
)

// startPty runs argv in a new pseudoconsole of the given size. The program is
// started with the pseudoconsole attached, so the console standard handles are
// the pseudoconsole itself.
func startPty(argv []string, env []string, dir string, width, height int16) (*pty, error) {
	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, fmt.Errorf("input pipe: %w", err)
	}
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		return nil, fmt.Errorf("output pipe: %w", err)
	}
	var hpc windows.Handle
	size := uintptr(uint16(width)) | uintptr(uint16(height))<<16
	hr, _, _ := procCreatePseudoConsole.Call(size, uintptr(inRead), uintptr(outWrite), 0,
		uintptr(unsafe.Pointer(&hpc)))
	if int32(hr) < 0 {
		return nil, fmt.Errorf("CreatePseudoConsole: 0x%x", uint32(hr))
	}
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, fmt.Errorf("attribute list: %w", err)
	}
	defer attrs.Delete()
	if err := attrs.Update(procThreadAttributePseudoConsole, unsafe.Pointer(&hpc), unsafe.Sizeof(hpc)); err != nil {
		return nil, fmt.Errorf("pseudoconsole attribute: %w", err)
	}
	si := &windows.StartupInfoEx{
		StartupInfo:             windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{}))},
		ProcThreadAttributeList: attrs.List(),
	}
	cmdline, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(argv))
	if err != nil {
		return nil, err
	}
	var dirp *uint16
	if dir != "" {
		if dirp, err = windows.UTF16PtrFromString(dir); err != nil {
			return nil, err
		}
	}
	var pi windows.ProcessInformation
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT)
	if err := windows.CreateProcess(nil, cmdline, nil, nil, false, flags,
		utf16Env(env), dirp, &si.StartupInfo, &pi); err != nil {
		return nil, fmt.Errorf("CreateProcess: %w", err)
	}
	p := &pty{
		hpc:    hpc,
		input:  os.NewFile(uintptr(inWrite), "pty-in"),
		output: os.NewFile(uintptr(outRead), "pty-out"),
		proc:   pi,
	}
	go p.drain()
	return p, nil
}

// utf16Env builds a UTF-16 environment block (sorted, double NUL terminated).
func utf16Env(env []string) *uint16 {
	var block []uint16
	for _, kv := range env {
		u, err := windows.UTF16FromString(kv)
		if err != nil {
			continue
		}
		block = append(block, u...)
	}
	block = append(block, 0)
	return &block[0]
}

// drain keeps reading the program's output: a terminal that stops reading would
// block the program once the pipe buffer fills.
func (p *pty) drain() {
	buf := make([]byte, 4096)
	for {
		n, err := p.output.Read(buf)
		if n > 0 {
			p.mu.Lock()
			p.buf = append(p.buf, buf[:n]...)
			p.mu.Unlock()
			if p.extra != nil {
				_, _ = p.extra.Write(buf[:n])
			}
		}
		if err != nil {
			return
		}
	}
}

// Write sends terminal input to the program.
func (p *pty) Write(s string) error {
	_, err := io.WriteString(p.input, s)
	return err
}

// Output is everything the program printed so far.
func (p *pty) Output() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return string(p.buf)
}

// WaitExit waits up to d for the program to end.
func (p *pty) WaitExit(d time.Duration) error {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		state, err := windows.WaitForSingleObject(p.proc.Process, 100)
		if err == nil && state == windows.WAIT_OBJECT_0 {
			var code uint32
			_ = windows.GetExitCodeProcess(p.proc.Process, &code)
			fmt.Printf("child exited with code %d (0x%x)\n", code, code)
			return nil
		}
	}
	return fmt.Errorf("still running after %s", d)
}

// Close ends the pseudoconsole and the program in it.
func (p *pty) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.mu.Unlock()
	_, _, _ = procClosePseudoConsole.Call(uintptr(p.hpc))
	_ = windows.TerminateProcess(p.proc.Process, 1)
	_ = windows.CloseHandle(p.proc.Process)
	_ = windows.CloseHandle(p.proc.Thread)
	_ = p.input.Close()
	_ = p.output.Close()
}
