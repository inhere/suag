//go:build !linux && !darwin

package chat

// restoreOutputProcessing is a no-op where the tty does not keep output flags in
// the same termios structure: the Windows console (and a ConPTY) handles CR/LF
// translation itself, raw console input included.
func restoreOutputProcessing(int) error { return nil }

// clearOutputProcessing is the no-op counterpart of restoreOutputProcessing: there
// are no output flags to take off on these systems either.
func clearOutputProcessing(int) error { return nil }
