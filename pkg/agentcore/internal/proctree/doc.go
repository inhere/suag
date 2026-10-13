// Package proctree kills a command's whole process tree: a process group on
// Unix, a job object (KILL_ON_JOB_CLOSE) on Windows. Call New before Start,
// Started right after Start, Kill to end the tree, Close once the command
// has exited (it also ends children left running in the background).
package proctree
