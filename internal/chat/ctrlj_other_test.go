//go:build !windows

package chat

// testNewline is the byte sequence a terminal hands over for Enter (and for the line
// break of a paste or a pre-typed line): Unix terminals turn CR into LF with ICRNL.
const testNewline = "\n"
