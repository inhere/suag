//go:build windows

package proctree

import (
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Tree is a job object holding the shell and every process it starts.
// The job is created with KILL_ON_JOB_CLOSE, so closing it ends leftovers.
// Between Start and the assignment a child could escape; that window is a
// few microseconds and accepted.
type Tree struct {
	cmd *exec.Cmd
	job windows.Handle
}

// New prepares cmd (before Start) so its whole process tree can be killed.
func New(cmd *exec.Cmd) *Tree {
	p := &Tree{cmd: cmd}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return p // fall back to killing the shell alone
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return p
	}
	p.job = job
	return p
}

// Started puts the shell into the job; children inherit the job.
func (p *Tree) Started() {
	if p.job == 0 || p.cmd.Process == nil {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(p.cmd.Process.Pid))
	if err != nil {
		return
	}
	_ = windows.AssignProcessToJobObject(p.job, h)
	_ = windows.CloseHandle(h)
}

// Kill terminates the whole job, and the shell directly in case it was not
// assigned yet.
func (p *Tree) Kill() error {
	if p.job != 0 {
		_ = windows.TerminateJobObject(p.job, 1)
	}
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	return nil
}

// Close releases the job; KILL_ON_JOB_CLOSE ends background leftovers.
func (p *Tree) Close() {
	if p.job != 0 {
		_ = windows.CloseHandle(p.job)
		p.job = 0
	}
}
