package chat

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestChatCompletion(t *testing.T) {
	t.Run("unique and multiple commands", func(t *testing.T) {
		source := &chatInputSource{output: &bytes.Buffer{}}
		line, pos, ok := completeCandidates(source, ":se", []string{":session"})
		assert.True(t, ok)
		assert.Eq(t, ":session", line)
		assert.Eq(t, len(line), pos)
		line, pos, ok = completeCandidates(source, ":", []string{":session", ":compact"})
		assert.True(t, ok)
		assert.Eq(t, ":", line)
		assert.Eq(t, 1, pos)
	})
	t.Run("directory with spaces", func(t *testing.T) {
		dir := t.TempDir()
		child := filepath.Join(dir, "folder with space")
		assert.NoErr(t, os.Mkdir(child, 0o700))
		source := &chatInputSource{output: &bytes.Buffer{}}
		line, pos, ok := completePath(source, ":add-dir "+dir+string(os.PathSeparator)+"fol", ":add-dir", dir+string(os.PathSeparator)+"fol")
		assert.True(t, ok)
		assert.Eq(t, ":add-dir "+child, line)
		assert.Eq(t, len(line), pos)
	})
}
