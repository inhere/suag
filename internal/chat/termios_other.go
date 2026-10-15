//go:build !linux && !darwin

package chat

// restoreOutputProcessing is a no-op where the tty does not keep output flags in
// the same termios structure: the Windows console (and a ConPTY) handles CR/LF
// translation itself, raw console input included.
func restoreOutputProcessing(int) error { return nil }
