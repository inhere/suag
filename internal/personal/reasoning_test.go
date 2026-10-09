package personal

import (
	"context"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/pkg/agentcore"
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
