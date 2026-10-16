package chat

import (
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/internal/personal"
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

func TestBuildTurnStatusBudget(t *testing.T) {
	statusLine := func(snapshot *personal.BudgetSnapshot) string {
		lines := BuildTurnStatus(TurnStatus{Model: "m", Budget: snapshot})
		assert.Require(t, len(lines) > 0)
		return lines[len(lines)-1].Text
	}
	t.Run("turn budget shows percentage", func(t *testing.T) {
		got := statusLine(&personal.BudgetSnapshot{Scope: "turn", Tokens: personal.BudgetMetric{Limit: 100, Used: 62}})
		assert.StrContains(t, got, "budget 62%")
	})
	t.Run("session budget and estimated values are marked", func(t *testing.T) {
		got := statusLine(&personal.BudgetSnapshot{Scope: "session", Estimated: true,
			Tokens: personal.BudgetMetric{Limit: 100, Used: 62}})
		assert.StrContains(t, got, "session budget ~62%")
	})
	t.Run("token budget wrap-up gives reason and limit", func(t *testing.T) {
		got := statusLine(&personal.BudgetSnapshot{Scope: "turn", Exceeded: true, Reason: "max_tokens",
			Tokens: personal.BudgetMetric{Limit: 5, Used: 12}})
		assert.StrContains(t, got, "本轮因 token 预算用尽收尾（5/12）")
	})
	t.Run("cost budget wrap-up gives dollar values", func(t *testing.T) {
		got := statusLine(&personal.BudgetSnapshot{Scope: "turn", Exceeded: true, Reason: "max_cost_usd",
			CostUSD: personal.BudgetMetric{Limit: 1.25, Used: 1.5}})
		assert.StrContains(t, got, "本轮因成本预算用尽收尾（$1.2500/$1.5000）")
	})
	t.Run("no budget leaves the old line unchanged", func(t *testing.T) {
		got := statusLine(nil)
		assert.StrNotContains(t, got, "budget")
	})
}
