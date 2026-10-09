//go:build !linux && !darwin && !windows

package chat

// flushTerminalInput is a no-op where no flush call is wired up; Ask still
// drops bytes already held by the reader.
func flushTerminalInput(int) error { return nil }
