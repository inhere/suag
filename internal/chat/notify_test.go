package chat

import (
	"bytes"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
)

func TestChatNotifierModes(t *testing.T) {
	cases := []struct {
		name, mode, env, want string
	}{
		{"auto bell", "auto", "", "\a"},
		{"auto osc9", "auto", "WT_SESSION", "\x1b]9;hello\a"},
		{"forced osc9", "osc9", "", "\x1b]9;hello\a"},
		{"off", "off", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			n := chatNotifier{mode: notifyMode(tc.mode), after: time.Second, out: &out, envWT: tc.env != ""}
			n.notify("hello")
			assert.Eq(t, tc.want, out.String())
		})
	}
	t.Run("threshold and off", func(t *testing.T) {
		n := chatNotifier{mode: notifyBell, after: time.Second, out: &bytes.Buffer{}}
		assert.False(t, n.shouldNotify(999*time.Millisecond))
		assert.True(t, n.shouldNotify(time.Second))
		n.mode = notifyOff
		assert.False(t, n.shouldNotify(time.Second))
	})
	t.Run("non terminal is silent", func(t *testing.T) {
		var out bytes.Buffer
		newChatNotifier("bell", time.Second, &out, false).notify("hello")
		assert.Empty(t, out.String())
	})
}
