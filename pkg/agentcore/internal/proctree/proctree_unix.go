//go:build !windows

package proctree

import (
	"os/exec"
	"syscall"
)

// Tree is the command's process group: the shell starts a new group,
// so killing -pid reaches every child it spawned.
type Tree struct{ cmd *exec.Cmd }

// New prepares cmd (before Start) so its whole process tree can be killed.
func New(cmd *exec.Cmd) *Tree {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &Tree{cmd: cmd}
}

// Started is a no-op: the group exists from Start.
func (p *Tree) Started() {}

// Kill sends SIGKILL to the whole group.
func (p *Tree) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
}

// Close kills children the command left running in the background.
func (p *Tree) Close() { _ = p.Kill() }
