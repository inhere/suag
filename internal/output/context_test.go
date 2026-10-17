package output

import (
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal"
)

func TestContextShare(t *testing.T) {
	for _, tc := range []struct {
		name              string
		lastInput, window int
		want              string
	}{
		{name: "rounds down to zero", lastInput: 9, window: 1000, want: "ctx 0%"},
		{name: "reports an oversized request", lastInput: 1050, window: 1000, want: "ctx 105%"},
		{name: "unknown window is omitted", lastInput: 550, window: 0, want: ""},
		{name: "no request is omitted", lastInput: 0, window: 1000, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Eq(t, tc.want, ContextShare(tc.lastInput, tc.window))
		})
	}
}

func TestWithContext(t *testing.T) {
	t.Run("adds used window and share", func(t *testing.T) {
		line := ResultLineWithContext("s", personal.RunResult{LastInputTokens: 550}, 1000)
		assert.Eq(t, map[string]any{"used": 550, "window": 1000, "share": 55}, line["context"])
	})
	t.Run("omits unknown context", func(t *testing.T) {
		line := WithContext(map[string]any{}, personal.RunResult{LastInputTokens: 550}, 0)
		_, ok := line["context"]
		assert.False(t, ok)
	})
}
