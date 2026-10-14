//go:build !windows

package chat

import "io"

// nativeTitleSupport: outside Windows there is no console API to fall back on, so
// the OSC 0 sequence is the only way to set the title and every host that gets
// it either interprets it or ignores it.
func nativeTitleSupport(io.Writer) (escape bool, native func(string) error) { return true, nil }
