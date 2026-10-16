package agentcore

import (
	"context"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// TestRuntime_Run_MaxOutputTokens: the runtime puts the output limit it was
// configured with into every request and reports a cut-off answer with it.
func TestRuntime_Run_MaxOutputTokens(t *testing.T) {
	run := func(t *testing.T, maxOutputTokens int, result *GenerateResult) (*runtimeTestProvider, *RunResult) {
		t.Helper()
		provider := &runtimeTestProvider{results: []*GenerateResult{result}}
		res, err := New(Config{
			Provider:        provider,
			Storage:         newRuntimeTestStorage(),
			PromptBuilder:   runtimeTestPromptBuilder{},
			ContextBuilder:  runtimeTestContextBuilder{},
			MaxOutputTokens: maxOutputTokens,
		}).Run(context.Background(), RunInput{RunID: newRunID(), AgentID: "agent-1", Instruction: "explore"})
		assert.Require(t, assert.NoErr(t, err))
		return provider, res
	}

	t.Run("the effective limit goes into the request", func(t *testing.T) {
		provider, res := run(t, 8000, &GenerateResult{Content: "done", FinishReason: FinishFinal})
		assert.Require(t, assert.Len(t, provider.calls, 1))
		assert.Eq(t, 8000, provider.calls[0].MaxTokens)
		assert.False(t, res.Truncated)
		assert.Eq(t, 0, res.OutputLimit)
	})
	t.Run("no configured limit means none is sent", func(t *testing.T) {
		provider, res := run(t, 0, &GenerateResult{Content: "done", FinishReason: FinishFinal})
		assert.Require(t, assert.Len(t, provider.calls, 1))
		assert.Eq(t, 0, provider.calls[0].MaxTokens)
		assert.False(t, res.Truncated)
	})
	t.Run("a cut-off answer carries the limit the provider named", func(t *testing.T) {
		_, res := run(t, 8000, &GenerateResult{Content: "half", FinishReason: FinishFinal, Truncated: true, OutputLimit: 4000})
		assert.True(t, res.Truncated)
		assert.Eq(t, 4000, res.OutputLimit)
		assert.Eq(t, "half", res.Output)
	})
	t.Run("without one from the provider the configured limit names the cut", func(t *testing.T) {
		_, res := run(t, 2000, &GenerateResult{Content: "half", FinishReason: FinishFinal, Truncated: true})
		assert.True(t, res.Truncated)
		assert.Eq(t, 2000, res.OutputLimit)
	})
	t.Run("an unknown limit stays 0 but the cut is reported", func(t *testing.T) {
		_, res := run(t, 0, &GenerateResult{Content: "half", FinishReason: FinishFinal, Truncated: true})
		assert.True(t, res.Truncated)
		assert.Eq(t, 0, res.OutputLimit)
	})
}
