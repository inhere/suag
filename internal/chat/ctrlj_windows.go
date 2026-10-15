//go:build windows

package chat

// ctrlJIsInputNewline: a bare LF inserts a newline in the input on Windows. A
// Windows console hands a cooked line over as CRLF (the pump drops that LF), and
// Ctrl+Enter cannot be told from Enter there (both arrive as CR), so Ctrl+J is the
// key that is free - and Windows Terminal keeps Alt+Enter for fullscreen.
const ctrlJIsInputNewline = true

// testNewline is the byte sequence a terminal hands over for Enter (and for the line
// break of a paste or a pre-typed line): a Windows console sends CRLF, and the pump
// drops the LF of that pair. A bare LF is Ctrl+J here (ctrlJIsInputNewline).
const testNewline = "\r\n"
