//go:build !linux && !darwin && !windows

package chat

import "time"

// waitTerminalInput has no implementation on this platform: reporting a byte as
// ready keeps the Esc byte what it is for x/term — the start of an escape sequence
// — instead of guessing a lone Esc from a wait this platform cannot do.
func waitTerminalInput(int, time.Duration) bool { return true }
