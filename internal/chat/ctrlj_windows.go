//go:build windows

package chat

// ctrlJIsInputNewline: a bare LF inserts a newline in the input on Windows, which
// covers Ctrl+J and Ctrl+Enter - a console hands a cooked line over as CRLF (the pump
// drops that LF), and Windows Terminal 1.24 sends Ctrl+Enter as a bare LF (measured
// with tmp/keyprobe). Shift+Enter arrives as CR there, i.e. like Enter itself, so it
// cannot be told apart without a terminal-side binding. Ctrl+Enter is the pair of keys
// that is free - and Windows Terminal keeps Alt+Enter for fullscreen.
const ctrlJIsInputNewline = true

// testNewline is the byte sequence a terminal hands over for Enter (and for the line
// break of a paste or a pre-typed line): a Windows console sends CRLF, and the pump
// drops the LF of that pair. A bare LF is Ctrl+J or Ctrl+Enter here
// (ctrlJIsInputNewline).
const testNewline = "\r\n"
