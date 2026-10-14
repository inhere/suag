//go:build !linux && !darwin && !windows

package chat

import "time"

// waitInputSupported is false here: waitTerminalInput cannot tell whether a byte
// is ready, so an answer of "nothing ready" is not trustworthy. A newline is then
// never taken for pasted text (the pump would insert one for every Enter, and no
// line could be submitted anymore).
const waitInputSupported = false

// waitTerminalInput has no implementation on this platform: reporting a byte as
// ready keeps the Esc byte what it is for x/term — the start of an escape sequence
// — and a newline what it is for chat — a submit — instead of guessing a decision
// from a wait this platform cannot do.
func waitTerminalInput(int, time.Duration) bool { return true }
