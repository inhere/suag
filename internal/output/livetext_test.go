package output

import (
	"bytes"
	"testing"
	"time"

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

func TestPrefixedLiveTextMarksEachMessage(t *testing.T) {
	var out bytes.Buffer
	live := NewPrefixedLiveText(&out, "▣ ")
	delta := func(s string) personal.Event {
		return personal.Event{Kind: string(agentcore.StreamEventTextDelta), Text: s}
	}
	stop := personal.Event{Kind: string(agentcore.StreamEventMessageStop)}
	live.Handle(delta("let me "))
	live.Handle(delta("check")) // same message: no second marker
	live.Handle(stop)
	live.Handle(delta("")) // an empty delta does not open a message
	live.Handle(delta("done"))
	live.Finish()
	assert.Eq(t, "▣ let me check\n▣ done\n", out.String())
}

func TestLiveTextRetry(t *testing.T) {
	var out bytes.Buffer
	live := NewPrefixedLiveText(&out, "▣ ")
	delta := func(s string) personal.Event {
		return personal.Event{Kind: string(agentcore.StreamEventTextDelta), Text: s}
	}
	retry := personal.Event{Kind: string(agentcore.StreamEventRetry), Text: "stream broken",
		Retry: &personal.RetryInfo{Attempt: 1, Wait: 1234 * time.Millisecond, Restart: true}}
	live.Handle(delta("Hel"))
	// The caller prints the notice: the line is closed and the next message
	// gets its marker again.
	assert.Eq(t, false, live.Handle(retry))
	out.WriteString(RetryNotice(retry) + "\n")
	live.Handle(delta("Hello"))
	live.Finish()
	assert.Eq(t, "▣ Hel\n↻ 模型请求失败（stream broken），1.2s 后重试（第 1 次）；上面的回复不完整，将重新输出\n▣ Hello\n", out.String())
	assert.Eq(t, "↻ 模型请求失败（HTTP 503），0s 后重试（第 0 次）", RetryNotice(personal.Event{Kind: "retry", Text: "HTTP 503"}))
}
