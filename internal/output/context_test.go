package output

import (
	"testing"

	"github.com/gookit/goutil/x/assert"
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
