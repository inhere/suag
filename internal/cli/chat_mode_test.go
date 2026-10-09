package cli

import (
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// TestWiringOptionsChatMode checks that only chat opens the runtime in chat
// mode, where tools.allow_write and the chat approver apply.
func TestWiringOptionsChatMode(t *testing.T) {
	opts := personalOptions{AllowWrite: true, ReadOnly: true}
	t.Run("chat", func(t *testing.T) {
		wopts := opts.wiringOptions("chat")
		assert.True(t, wopts.ChatMode)
		assert.True(t, wopts.AllowWrite)
		assert.True(t, wopts.ReadOnly)
	})
	t.Run("run", func(t *testing.T) {
		assert.False(t, opts.wiringOptions("run").ChatMode)
	})
}
