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
	load := chatConfigLoader(t)
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

// TestChatSpinnerEnabled: chat.spinner takes the same values as chat.markdown
// and is on by default; each key decides on its own.
func TestChatSpinnerEnabled(t *testing.T) {
	load := chatConfigLoader(t)
	t.Run("default on", func(t *testing.T) {
		load(t, "model: openai/m\n")
		assert.True(t, ChatSpinnerEnabled())
	})
	t.Run("bool off", func(t *testing.T) {
		load(t, "chat:\n  spinner: false\n")
		assert.False(t, ChatSpinnerEnabled())
	})
	t.Run("string off", func(t *testing.T) {
		load(t, "chat:\n  spinner: \"off\"\n")
		assert.False(t, ChatSpinnerEnabled())
	})
	t.Run("bool on", func(t *testing.T) {
		load(t, "chat:\n  spinner: true\n")
		assert.True(t, ChatSpinnerEnabled())
	})
	t.Run("markdown does not decide it", func(t *testing.T) {
		load(t, "chat:\n  markdown: false\n")
		assert.True(t, ChatSpinnerEnabled())
	})
}

// TestChatHighlightInputEnabled: chat.highlight_input takes the same values as
// chat.markdown, is on by default and is decided on its own.
func TestChatHighlightInputEnabled(t *testing.T) {
	load := chatConfigLoader(t)
	t.Run("default on", func(t *testing.T) {
		load(t, "model: openai/m\n")
		assert.True(t, ChatHighlightInputEnabled())
	})
	t.Run("bool off", func(t *testing.T) {
		load(t, "chat:\n  highlight_input: false\n")
		assert.False(t, ChatHighlightInputEnabled())
	})
	t.Run("string off", func(t *testing.T) {
		load(t, "chat:\n  highlight_input: \"off\"\n")
		assert.False(t, ChatHighlightInputEnabled())
	})
	t.Run("bool on", func(t *testing.T) {
		load(t, "chat:\n  highlight_input: true\n")
		assert.True(t, ChatHighlightInputEnabled())
	})
	t.Run("spinner does not decide it", func(t *testing.T) {
		load(t, "chat:\n  spinner: false\n")
		assert.True(t, ChatHighlightInputEnabled())
	})
}

// TestChatTerminalTitleEnabled: chat.terminal_title takes the same values as
// chat.markdown, is on by default and is decided on its own.
func TestChatTerminalTitleEnabled(t *testing.T) {
	load := chatConfigLoader(t)
	t.Run("default on", func(t *testing.T) {
		load(t, "model: openai/m\n")
		assert.True(t, ChatTerminalTitleEnabled())
	})
	t.Run("bool off", func(t *testing.T) {
		load(t, "chat:\n  terminal_title: false\n")
		assert.False(t, ChatTerminalTitleEnabled())
	})
	t.Run("string off", func(t *testing.T) {
		load(t, "chat:\n  terminal_title: \"off\"\n")
		assert.False(t, ChatTerminalTitleEnabled())
	})
	t.Run("bool on", func(t *testing.T) {
		load(t, "chat:\n  terminal_title: true\n")
		assert.True(t, ChatTerminalTitleEnabled())
	})
	t.Run("spinner does not decide it", func(t *testing.T) {
		load(t, "chat:\n  spinner: false\n")
		assert.True(t, ChatTerminalTitleEnabled())
	})
}

// TestChatAskOutsideDirs: chat.ask_outside_dirs takes the same values as
// chat.markdown, is on by default and is decided on its own.
func TestChatAskOutsideDirs(t *testing.T) {
	load := chatConfigLoader(t)
	t.Run("default on", func(t *testing.T) {
		load(t, "model: openai/m\n")
		assert.True(t, ChatAskOutsideDirs())
	})
	t.Run("bool off", func(t *testing.T) {
		load(t, "chat:\n  ask_outside_dirs: false\n")
		assert.False(t, ChatAskOutsideDirs())
	})
	t.Run("string off", func(t *testing.T) {
		load(t, "chat:\n  ask_outside_dirs: \"off\"\n")
		assert.False(t, ChatAskOutsideDirs())
	})
	t.Run("bool on", func(t *testing.T) {
		load(t, "chat:\n  ask_outside_dirs: true\n")
		assert.True(t, ChatAskOutsideDirs())
	})
	t.Run("terminal_title does not decide it", func(t *testing.T) {
		load(t, "chat:\n  terminal_title: false\n")
		assert.True(t, ChatAskOutsideDirs())
	})
}

// chatConfigLoader returns a helper writing body to a fresh SUGA_HOME's
// config.yaml and loading it.
func chatConfigLoader(t *testing.T) func(t *testing.T, body string) {
	t.Helper()
	return func(t *testing.T, body string) {
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
}
