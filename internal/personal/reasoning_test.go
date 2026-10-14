package personal

import (
	"context"
	"fmt"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore"
)

// The model's reasoning is stored on the assistant messages (tool-call and
// final) and, after a reload from the store, still reaches the next turn's
// request, as thinking-mode APIs with tools require.
func TestReasoningPersistsAcrossTurns(t *testing.T) {
	repo := openObserveRepo(t)
	prov := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		switch n {
		case 1:
			res, _ := callTool("c1")
			res.Reasoning = "think-call"
			return res, nil
		case 2:
			res, _ := finalText("answer")
			res.Reasoning = "think-final"
			return res, nil
		default:
			return finalText("again")
		}
	}}
	runner := compactRunner(t, repo, prov, bigToolRegistry(10, nil), func(c *Config) { c.ContextWindow, c.CompactReserveTokens = 100000, 1000 })
	res, err := runner.Run(context.Background(), RunRequest{Prompt: "first"})
	assert.NoErr(t, err)
	_, err = runner.Run(context.Background(), RunRequest{SessionID: res.SessionID, Prompt: "second"})
	assert.NoErr(t, err)

	reasoningOf := func(req *agentcore.GenerateRequest) []string {
		var out []string
		for _, m := range req.Messages {
			if m.Role == "assistant" {
				out = append(out, m.Reasoning)
			}
		}
		return out
	}
	assert.Eq(t, 3, len(prov.reqs))
	assert.Eq(t, []string{"think-call"}, reasoningOf(prov.reqs[1]))
	assert.Eq(t, []string{"think-call", "think-final"}, reasoningOf(prov.reqs[2]))
}

// Reaching max_steps asks for a wrap-up answer instead of failing the turn.
func TestRunnerWrapsUpAtMaxSteps(t *testing.T) {
	repo := openObserveRepo(t)
	prov := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		if n <= 2 {
			return callTool(fmt.Sprintf("c%d", n))
		}
		return finalText("partial findings")
	}}
	runner := compactRunner(t, repo, prov, bigToolRegistry(10, nil), func(c *Config) {
		c.ContextWindow, c.CompactReserveTokens, c.MaxSteps = 100000, 1000, 2
	})
	res, err := runner.Run(context.Background(), RunRequest{Prompt: "explore"})
	assert.NoErr(t, err)
	assert.Eq(t, "partial findings", res.Output)
	assert.Eq(t, "max_steps", res.WrapUp)
	assert.Eq(t, 3, len(prov.reqs))
	last := prov.reqs[2].Messages[len(prov.reqs[2].Messages)-1]
	assert.StrContains(t, last.Content, "步数上限（2 步）")
	// The report must name that reason instead of guessing another one.
	assert.StrContains(t, last.Content, "如实写明")
	assert.StrContains(t, last.Content, "不得写成超时")
}

// The step-limit instruction carries the limit and asks for the real reason; it
// is not sent at all when the step limit is off.
func TestMaxStepsWrapUpInstruction(t *testing.T) {
	t.Run("no instruction without a step limit", func(t *testing.T) {
		assert.Eq(t, "", maxStepsWrapUp(0))
		assert.Eq(t, "", maxStepsWrapUp(-1))
	})
	t.Run("the instruction names the limit and the real reason", func(t *testing.T) {
		got := maxStepsWrapUp(30)
		assert.StrContains(t, got, "步数上限（30 步）")
		assert.StrContains(t, got, "如实写明")
		assert.StrContains(t, got, "不得写成超时")
	})
}
