package agentcore

import (
	"context"
	"errors"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/pkg/agentcore/tools"
)

type emptyFinalModelHook struct {
	NopHook
	result *GenerateResult
}

func (h *emptyFinalModelHook) AfterModel(_ context.Context, event *ModelEvent, _ *Control) error {
	if event != nil {
		h.result = cloneGenerateResultValue(event.Result)
	}
	return nil
}

func TestRuntimeAllowsEmptyFinalAfterSuccessfulTool(t *testing.T) {
	store := newRuntimeTestStorage()
	reg := tools.NewRegistry()
	reg.MustRegister(&runtimeTestTool{name: "write", result: &ToolResult{Content: "written"}})
	hook := &emptyFinalModelHook{}
	empty := &GenerateResult{FinishReason: FinishFinal, Reasoning: "thinking only"}
	provider := &runtimeTestProvider{results: []*GenerateResult{
		{ToolCalls: []ToolCall{{CallID: "call-1", Name: "write"}}, FinishReason: FinishToolCalls},
		empty, empty, empty,
	}}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          reg,
		Hooks:          []Hook{hook},
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{RunID: "run-empty-after-tool", Instruction: "write it"})

	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, RunCompleted, result.Status)
	assert.Eq(t, "", result.Output)
	assert.True(t, result.EmptyFinal)
	assert.Len(t, provider.calls, 4)
	assert.Require(t, assert.NotNil(t, hook.result))
	assert.Eq(t, true, hook.result.Metadata[EmptyFinalMetaKey])
}

func TestRuntimeRetriesEmptyResult(t *testing.T) {
	run := func(t *testing.T, provider *runtimeTestProvider) (*RunResult, error) {
		rt := New(Config{
			Provider:       provider,
			Storage:        newRuntimeTestStorage(),
			Tools:          tools.NewRegistry(),
			PromptBuilder:  runtimeTestPromptBuilder{},
			ContextBuilder: runtimeTestContextBuilder{},
		})
		return rt.Run(context.Background(), RunInput{RunID: "run-empty", AgentID: "agent-1", Instruction: "hi"})
	}
	empty := &GenerateResult{FinishReason: FinishFinal, Reasoning: "thinking only"}

	t.Run("empty replies are re-sampled", func(t *testing.T) {
		provider := &runtimeTestProvider{results: []*GenerateResult{empty, empty, {Content: "done", FinishReason: FinishFinal}}}
		result, err := run(t, provider)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "done", result.Output)
		assert.Len(t, provider.calls, 3)
	})
	t.Run("the run fails after the retries are used up", func(t *testing.T) {
		provider := &runtimeTestProvider{results: []*GenerateResult{empty, empty, empty, {Content: "late", FinishReason: FinishFinal}}}
		_, err := run(t, provider)
		assert.True(t, errors.Is(err, ErrEmptyGenerateResult), "err = %v", err)
		assert.Len(t, provider.calls, 1+emptyResultRetries)
	})
	t.Run("other invalid results are not retried", func(t *testing.T) {
		provider := &runtimeTestProvider{results: []*GenerateResult{{Content: "x"}, {Content: "done", FinishReason: FinishFinal}}}
		_, err := run(t, provider)
		assert.True(t, errors.Is(err, ErrMissingFinishReason), "err = %v", err)
		assert.Len(t, provider.calls, 1)
	})
}
