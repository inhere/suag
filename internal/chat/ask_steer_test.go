package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestChatInputSource_AskTwiceInOneSteerReadShowsBothQuestions(t *testing.T) {
	h := newAskHarness(t)
	h.source.SetSteering(true)
	h.source.SetBusy(true)
	_, ok := h.source.RequestSteer(steerPrompt)
	assert.True(t, ok)
	waitUntil(t, "steer read", func() bool { return h.kb.Reading() > 0 })

	for i, tc := range []struct {
		prompt string
		answer string
	}{
		{prompt: "first question? ", answer: "a"},
		{prompt: "second question? ", answer: ""},
	} {
		done := make(chan askResult, 1)
		go func(prompt string) {
			line, err := h.source.Ask(context.Background(), "", prompt)
			done <- askResult{line, err}
		}(tc.prompt)
		waitUntil(t, tc.prompt, func() bool { return strings.Contains(h.out.String(), tc.prompt) })
		h.kb.Type(tc.answer + "\r")
		res := h.answer(t, done)
		assert.NoErr(t, res.err)
		assert.Eq(t, tc.answer, res.line)
		if i == 0 {
			assert.StrContains(t, h.out.String(), tc.prompt+tc.answer)
		}
	}
}
