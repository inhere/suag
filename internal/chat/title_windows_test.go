//go:build windows

package chat

import (
	"bytes"
	"os"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// TestNativeTitleSupport: only a Windows console whose output mode lacks virtual
// terminal processing takes the console API route; everything else - a host that
// passes the output through, a pipe, a plain file, a test buffer - counts as able
// to read the OSC sequence.
func TestNativeTitleSupport(t *testing.T) {
	t.Run("a buffer is no console", func(t *testing.T) {
		escape, native := nativeTitleSupport(&bytes.Buffer{})
		assert.True(t, escape)
		assert.Nil(t, native)
	})
	t.Run("a plain file is no console", func(t *testing.T) {
		file, err := os.CreateTemp(t.TempDir(), "not-a-console")
		assert.NoErr(t, err)
		defer func() { _ = file.Close() }()
		escape, native := nativeTitleSupport(file)
		assert.True(t, escape)
		assert.Nil(t, native)
	})
}
