//go:build !windows

package wiring

// renameConflict reports whether err is a platform refusal that a retry may
// clear. Only Windows refuses to rename a directory another process has a
// handle on, so nothing is retried here and the Unix behaviour is unchanged.
func renameConflict(error) bool { return false }
