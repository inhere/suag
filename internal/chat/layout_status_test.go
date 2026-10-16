package chat

import (
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/pkg/agentcore"
)

// TestBuildTurnStatusOutputLimit: a turn whose answer the model cut off at the
// output token limit says so in the status line (with the limit when one is
// known), and a normal turn's line stays unchanged.
func TestBuildTurnStatusOutputLimit(t *testing.T) {
	statusLine := func(s TurnStatus) string {
		lines := BuildTurnStatus(s)
		assert.Require(t, len(lines) > 0)
		return lines[len(lines)-1].Text
	}
	t.Run("a normal turn names no limit", func(t *testing.T) {
		assert.StrNotContains(t, statusLine(TurnStatus{Model: "m", Usage: agentcore.Usage{OutputTokens: 3}}), "输出达到上限")
	})
	t.Run("a cut-off answer names the limit", func(t *testing.T) {
		assert.StrContains(t, statusLine(TurnStatus{Model: "m", Truncated: true, OutputLimit: 8000}), "输出达到上限（8000 tokens），回答被截断")
	})
	t.Run("an unknown limit still reports the cut", func(t *testing.T) {
		assert.StrContains(t, statusLine(TurnStatus{Model: "m", Truncated: true}), "输出达到上限，回答被截断")
	})
}
