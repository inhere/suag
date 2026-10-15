package chat

import (
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// TestSetBusySteeringKeepsRawInput: with chat.steering a turn keeps the keyboard
// readable (runTurn starts a steer read), so SetBusy(true) enters raw input instead
// of forcing cooked mode; the end of the turn hands the terminal back. Without
// steering a turn has to stay cooked, so Ctrl-C still arrives as SIGINT. The output
// flags follow the read the raw mode is held for: only a turn's read gets OPOST|ONLCR
// back (outputProcessingForRawLocked), a prompt read keeps what raw input set.
func TestSetBusySteeringKeepsRawInput(t *testing.T) {
	newSource := func(steering bool) (source *chatInputSource, raw, restored *int, output *[]string) {
		raw, restored = new(int), new(int)
		calls := new([]string)
		source = &chatInputSource{steering: steering}
		source.makeRaw = func() (func() error, error) {
			*raw++
			return func() error { *restored++; return nil }, nil
		}
		source.restoreOutput = func() error { *calls = append(*calls, "restore"); return nil }
		source.clearOutput = func() error { *calls = append(*calls, "clear"); return nil }
		return source, raw, restored, calls
	}

	t.Run("steering on: raw input stays for the turn", func(t *testing.T) {
		source, raw, restored, output := newSource(true)
		source.SetBusy(true)
		assert.Eq(t, 1, *raw)
		assert.NotNil(t, source.restoreRaw)
		assert.Eq(t, []string{"restore"}, *output)
		// The end of a turn does not hand the terminal back: the input line stays
		// available (the turn's read goes on as the next prompt's), and the read that
		// put the tty into raw mode restores it when it returns (readLine's leaveRaw).
		// Its output flags are not touched here either - the read that goes on sets
		// them when the prompt asks for it (the subtest below).
		source.SetBusy(false)
		assert.Eq(t, 0, *restored)
		assert.NotNil(t, source.restoreRaw)
		assert.Eq(t, []string{"restore"}, *output)
	})

	t.Run("steering on: the next prompt read clears them", func(t *testing.T) {
		// The read the turn held goes on as the prompt's: it is that read that needs
		// the plain raw flags, so they are set when the prompt asks for it (request)
		// - before it reads anything and before its prompt is repainted - and not at
		// the end of the turn (what is written to the terminal straight right after a
		// turn keeps its newlines).
		source, raw, restored, output := newSource(true)
		source.SetBusy(true)
		source.SetBusy(false)
		assert.Eq(t, []string{"restore"}, *output)
		source.mu.Lock()
		source.pending = true // the turn's read is still waiting for input
		source.mu.Unlock()
		assert.True(t, source.Request("> "))
		assert.Eq(t, []string{"restore", "clear"}, *output)
		assert.Eq(t, 1, *raw)      // the read continued, no new raw mode
		assert.Eq(t, 0, *restored) // nor a restore of the terminal
	})

	t.Run("steering off: the turn keeps cooked mode", func(t *testing.T) {
		source, raw, restored, output := newSource(false)
		source.SetBusy(true)
		assert.Eq(t, 0, *raw)
		assert.Eq(t, 0, *restored)
		assert.Nil(t, source.restoreRaw)
		// No raw input is held: the terminal stays cooked and its output flags are
		// the ones that mode has.
		assert.Eq(t, 0, len(*output))
	})

	t.Run("steering on: a read entered during the turn restores them again", func(t *testing.T) {
		// The turn's read ends with a submitted message and the next one starts
		// while the turn goes on: the fresh raw mode needs OPOST|ONLCR back, or the
		// output written to the terminal straight during the rest of the turn would
		// keep its column.
		source, _, _, output := newSource(true)
		source.SetBusy(true)
		assert.NoErr(t, source.leaveRaw())
		assert.NoErr(t, source.enterRaw())
		assert.Eq(t, []string{"restore", "restore"}, *output)
	})

	t.Run("steering on: a read from before the turn switches with it", func(t *testing.T) {
		// A delivery starts the turn while the prompt's read is still waiting: that
		// read goes on as the turn's own, without a new raw mode, so the flags
		// change in SetBusy itself.
		source, raw, _, output := newSource(true)
		assert.NoErr(t, source.enterRaw()) // the prompt's read
		assert.Eq(t, 1, *raw)
		assert.Eq(t, []string{"clear"}, *output)
		source.SetBusy(true)
		assert.Eq(t, 1, *raw)
		assert.Eq(t, []string{"clear", "restore"}, *output)
	})

	t.Run("steering off: a question's raw read stays plain", func(t *testing.T) {
		// Ask enters raw mode for the answer of an approval question with the turn
		// running: it is no turn read, so the flags stay as raw input set them.
		// Ask clears turnActive for the question (ask.go).
		source, raw, _, output := newSource(false)
		source.SetBusy(true)
		source.mu.Lock()
		source.turnActive = false
		source.mu.Unlock()
		assert.NoErr(t, source.enterRaw())
		assert.Eq(t, 1, *raw)
		assert.Eq(t, []string{"clear"}, *output)
	})
}
