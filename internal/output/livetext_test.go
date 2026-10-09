package output

import (
	"bytes"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/pkg/agentcore"
)

func TestLiveTextPrintsDeltasPerMessage(t *testing.T) {
	var out bytes.Buffer
	live := NewLiveText(&out)
	delta := func(s string) personal.Event {
		return personal.Event{Kind: string(agentcore.StreamEventTextDelta), Text: s}
	}
	stop := personal.Event{Kind: string(agentcore.StreamEventMessageStop)}
	assert.Eq(t, true, live.Handle(delta("let me ")))
	live.Handle(delta("check"))
	live.Handle(stop) // a tool call follows: the commentary gets its own line
	live.Handle(delta("done\n"))
	live.Handle(stop)
	assert.Eq(t, false, live.Handle(personal.Event{Kind: "tool_result"}))
	assert.Eq(t, true, live.Finish())
	assert.Eq(t, "let me check\ndone\n", out.String())
	assert.Eq(t, false, NewLiveText(&out).Finish())
}
