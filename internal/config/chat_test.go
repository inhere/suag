package axconfig

import (
	"os"
	"path/filepath"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
)

// TestChatMarkdownEnabled: chat.markdown is on unless it is turned off; the key
// takes a bool or a string.
func TestChatMarkdownEnabled(t *testing.T) {
	load := func(t *testing.T, body string) {
		t.Helper()
		gkconfig.Reset()
		t.Cleanup(gkconfig.Reset)
		root := t.TempDir()
		t.Setenv("SUGA_HOME", root)
		t.Setenv("SUGA_CONFIG", "")
		t.Chdir(t.TempDir())
		assert.NoErr(t, os.WriteFile(filepath.Join(root, "config.yaml"), []byte(body), 0600))
		assert.NoErr(t, Load())
	}
	t.Run("default on", func(t *testing.T) {
		load(t, "model: openai/m\n")
		assert.True(t, ChatMarkdownEnabled())
	})
	t.Run("bool off", func(t *testing.T) {
		load(t, "chat:\n  markdown: false\n")
		assert.False(t, ChatMarkdownEnabled())
	})
	t.Run("bool on", func(t *testing.T) {
		load(t, "chat:\n  markdown: true\n")
		assert.True(t, ChatMarkdownEnabled())
	})
	t.Run("string off", func(t *testing.T) {
		load(t, "chat:\n  markdown: off\n")
		assert.False(t, ChatMarkdownEnabled())
	})
	t.Run("other chat keys do not decide it", func(t *testing.T) {
		load(t, "chat:\n  other: 1\n")
		assert.True(t, ChatMarkdownEnabled())
	})
}
