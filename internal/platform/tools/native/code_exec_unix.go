//go:build !windows

package native

import (
	"os/exec"
	"syscall"
)

// processTree is the command's process group: the shell starts a new group,
// so killing -pid reaches every child it spawned.
type processTree struct{ cmd *exec.Cmd }

func newProcessTree(cmd *exec.Cmd) *processTree {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &processTree{cmd: cmd}
}

// started is a no-op: the group exists from Start.
func (p *processTree) started() {}

// kill sends SIGKILL to the whole group.
func (p *processTree) kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
}

// close kills children the command left running in the background.
func (p *processTree) close() { _ = p.kill() }
