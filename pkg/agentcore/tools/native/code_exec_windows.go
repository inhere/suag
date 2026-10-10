//go:build windows

package native

import (
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processTree is a job object holding the shell and every process it starts.
// The job is created with KILL_ON_JOB_CLOSE, so closing it ends leftovers.
// Between Start and the assignment a child could escape; that window is a
// few microseconds and accepted.
type processTree struct {
	cmd *exec.Cmd
	job windows.Handle
}

func newProcessTree(cmd *exec.Cmd) *processTree {
	p := &processTree{cmd: cmd}
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

// started puts the shell into the job; children inherit the job.
func (p *processTree) started() {
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

// kill terminates the whole job, and the shell directly in case it was not
// assigned yet.
func (p *processTree) kill() error {
	if p.job != 0 {
		_ = windows.TerminateJobObject(p.job, 1)
	}
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	return nil
}

// close releases the job; KILL_ON_JOB_CLOSE ends background leftovers.
func (p *processTree) close() {
	if p.job != 0 {
		_ = windows.CloseHandle(p.job)
		p.job = 0
	}
}
