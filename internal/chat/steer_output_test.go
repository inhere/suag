package chat

import (
	"bytes"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// TestSteerOutFollowsTheInputLine: output that may land while the input line is on
// screen (hook notices, errors, the process log) goes through the input source
// while the line is up - lineedit then prints it above the line and repaints the
// line - and to its own stream otherwise (design N4).
func TestSteerOutFollowsTheInputLine(t *testing.T) {
	t.Run("a pending steer read routes it through the input source", func(t *testing.T) {
		h := newAskHarness(t)
		h.source.SetSteering(true)
		fallback := &bytes.Buffer{}
		out := steerOut{source: h.source, fallback: fallback}

		// Nothing is read yet: the line is not on screen, so the writer keeps its own stream.
		assert.False(t, h.source.lineOnScreen())
		_, err := out.Write([]byte("回合之前\n"))
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "回合之前\n", fallback.String())

		// A turn keeps a read open: the input line is on screen from then on.
		assert.True(t, h.source.Request("> "))
		waitUntil(t, "the line is on screen", h.source.lineOnScreen)
		n, err := out.Write([]byte("回合之中\n"))
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, len("回合之中\n"), n)
		assert.StrContains(t, h.out.String(), "回合之中")
		assert.Eq(t, "回合之前\n", fallback.String()) // still only the earlier line
	})

	t.Run("a finished read falls back to its own stream", func(t *testing.T) {
		h := newAskHarness(t)
		h.source.SetSteering(true)
		fallback := &bytes.Buffer{}
		out := steerOut{source: h.source, fallback: fallback}

		assert.True(t, h.source.Request("> "))
		waitUntil(t, "the line is on screen", h.source.lineOnScreen)
		h.source.CompleteLine() // the chat loop took the line: the read is over

		assert.False(t, h.source.lineOnScreen())
		_, err := out.Write([]byte("回合之后\n"))
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "回合之后\n", fallback.String())
	})

	t.Run("without steering nothing is routed", func(t *testing.T) {
		h := newAskHarness(t)
		fallback := &bytes.Buffer{}
		out := steerOut{source: h.source, fallback: fallback}

		assert.True(t, h.source.Request("> "))
		assert.False(t, h.source.lineOnScreen())
		_, err := out.Write([]byte("旧的提示符读\n"))
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "旧的提示符读\n", fallback.String())
	})
}
