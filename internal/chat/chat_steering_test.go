package chat

import (
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// TestSetBusySteeringKeepsRawInput: with chat.steering a turn keeps the keyboard
// readable (runTurn starts a steer read), so SetBusy(true) enters raw input instead
// of forcing cooked mode; the end of the turn hands the terminal back. Without
// steering a turn has to stay cooked, so Ctrl-C still arrives as SIGINT.
func TestSetBusySteeringKeepsRawInput(t *testing.T) {
	newSource := func(steering bool) (source *chatInputSource, raw, restored *int) {
		raw, restored = new(int), new(int)
		source = &chatInputSource{steering: steering}
		source.makeRaw = func() (func() error, error) {
			*raw++
			return func() error { *restored++; return nil }, nil
		}
		return source, raw, restored
	}

	t.Run("steering on: raw input stays for the turn", func(t *testing.T) {
		source, raw, restored := newSource(true)
		source.SetBusy(true)
		assert.Eq(t, 1, *raw)
		assert.NotNil(t, source.restoreRaw)
		source.SetBusy(false)
		assert.Eq(t, 1, *restored)
		assert.Nil(t, source.restoreRaw)
	})

	t.Run("steering off: the turn keeps cooked mode", func(t *testing.T) {
		source, raw, restored := newSource(false)
		source.SetBusy(true)
		assert.Eq(t, 0, *raw)
		assert.Eq(t, 0, *restored)
		assert.Nil(t, source.restoreRaw)
	})
}
