package agentcore

import (
	"context"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/pkg/agentcore/tools"
)

// wrapUpTestProvider is a scripted provider that also declares tool_choice
// support, so a test can tell the tool-less wrap-up retry from the fallback of
// a provider that takes no tool_choice.
type wrapUpTestProvider struct {
	runtimeTestProvider
	supportsToolChoice bool
}

// SupportsToolChoice implements ToolChooser.
func (p *wrapUpTestProvider) SupportsToolChoice() bool { return p.supportsToolChoice }

// TestRuntime_Run_IgnoredWrapUp covers a wrap-up step whose model asked for
// tools again instead of answering (suga-p5mt): the turn ends with an answer
// (text of the reply, the tool-less retry, or a fixed notice) and reports the
// wrap-up reason, instead of failing and losing the work of the run.
func TestRuntime_Run_IgnoredWrapUp(t *testing.T) {
	// A step limit of 1 makes the second model reply the wrap-up step.
	const runID = "run-ignored-wrap-up"

	newRun := func(t *testing.T, supportsToolChoice bool, results ...*GenerateResult) (*runtimeTestStorage, *wrapUpTestProvider, *RunResult, error) {
		t.Helper()
		store := newRuntimeTestStorage()
		provider := &wrapUpTestProvider{
			runtimeTestProvider: runtimeTestProvider{results: results},
			supportsToolChoice:  supportsToolChoice,
		}
		reg := tools.NewRegistry()
		reg.MustRegister(&runtimeTestTool{name: "lookup"})
		res, err := New(Config{
			Provider:       provider,
			Storage:        store,
			Tools:          reg,
			PromptBuilder:  runtimeTestPromptBuilder{},
			ContextBuilder: runtimeTestContextBuilder{},
			MaxSteps:       1,
			MaxStepsWrapUp: "WRAP UP",
		}).Run(context.Background(), RunInput{RunID: runID, AgentID: "agent-1", Instruction: "explore"})
		return store, provider, res, err
	}

	// The reply of an ordinary step: it calls the tool and asks for more.
	call := func(content string) *GenerateResult {
		return &GenerateResult{
			Content:      content,
			ToolCalls:    []ToolCall{{CallID: "call-" + content, Name: "lookup"}},
			FinishReason: FinishToolCalls,
			Usage:        Usage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
		}
	}

	t.Run("text alongside the tool calls answers the turn", func(t *testing.T) {
		store, provider, res, err := newRun(t, true, call(""), call("summary of what I found"))
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, RunCompleted, res.Status)
		assert.Eq(t, "max_steps", res.WrapUp)
		assert.Eq(t, "summary of what I found", res.Output)
		// The calls of the wrap-up reply did not run and no retry went out.
		assert.Eq(t, 2, len(provider.calls))
		// user, assistant+tool result of step 1, the answer: nothing else.
		msgs := store.messages[runID]
		assert.Require(t, assert.Len(t, msgs, 4))
		assert.Eq(t, "summary of what I found", msgs[3].Content)
		assert.Nil(t, msgs[3].ToolCall)
		assert.Eq(t, RunCompleted, store.runs[runID].Status)
	})

	t.Run("without text a tool-less retry answers", func(t *testing.T) {
		store, provider, res, err := newRun(t, true, call(""), call(""), &GenerateResult{
			Content:      "late report",
			FinishReason: FinishFinal,
			Usage:        Usage{InputTokens: 5, OutputTokens: 2, TotalTokens: 7},
		})
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, RunCompleted, res.Status)
		assert.Eq(t, "max_steps", res.WrapUp)
		assert.Eq(t, "late report", res.Output)

		assert.Require(t, assert.Len(t, provider.calls, 3))
		// Only the retry forbids tools; the wrap-up request itself is unchanged.
		assert.Eq(t, "", provider.calls[1].ToolChoice)
		assert.Eq(t, ToolChoiceNone, provider.calls[2].ToolChoice)
		// The retry carries the same messages and the same instruction.
		assert.Eq(t, len(provider.calls[1].Messages), len(provider.calls[2].Messages))
		last := provider.calls[2].Messages[len(provider.calls[2].Messages)-1]
		assert.Eq(t, "WRAP UP", last.Content)
		// Its usage counts (2+2+7 tokens of the three replies), its model call
		// does not: the limit is not re-checked.
		assert.Eq(t, 11, res.Usage.TotalTokens)
		assert.Eq(t, 2, res.Steps)
		assert.Eq(t, "late report", store.messages[runID][3].Content)
	})

	t.Run("still without text the fixed notice answers", func(t *testing.T) {
		store, provider, res, err := newRun(t, true, call("let me look"), call(""), call(""))
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, RunCompleted, res.Status)
		assert.Eq(t, "max_steps", res.WrapUp)
		assert.StrContains(t, res.Output, "本轮因步数上限（1 步）收尾")
		assert.StrContains(t, res.Output, "模型未给出汇报")
		// The last text of the run is repeated, so it is not lost.
		assert.StrContains(t, res.Output, "let me look")
		assert.Require(t, assert.Len(t, provider.calls, 3))
		assert.Eq(t, ToolChoiceNone, provider.calls[2].ToolChoice)
		// The notice is the run's persisted answer.
		assert.Eq(t, res.Output, store.messages[runID][len(store.messages[runID])-1].Content)
	})

	t.Run("a provider without tool_choice goes straight to the notice", func(t *testing.T) {
		_, provider, res, err := newRun(t, false, call(""), call(""))
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, RunCompleted, res.Status)
		assert.Eq(t, "max_steps", res.WrapUp)
		assert.StrContains(t, res.Output, "本轮因步数上限（1 步）收尾")
		// No retry: only the wrap-up reply (the step-1 call and the wrap-up one).
		assert.Eq(t, 2, len(provider.calls))
	})

	t.Run("a hook's reason is kept", func(t *testing.T) {
		// A hook asks for the wrap-up after the first model call; the reason it
		// gave must reach the result and the notice.
		reg := tools.NewRegistry()
		reg.MustRegister(&runtimeTestTool{name: "lookup"})
		provider := &runtimeTestProvider{results: []*GenerateResult{call(""), call("")}}
		store := newRuntimeTestStorage()
		res, err := New(Config{
			Provider:       provider,
			Storage:        store,
			Tools:          reg,
			PromptBuilder:  runtimeTestPromptBuilder{},
			ContextBuilder: runtimeTestContextBuilder{},
			MaxSteps:       10,
			MaxStepsWrapUp: "WRAP UP",
			Hooks:          []Hook{&wrapUpHook{at: 1}},
		}).Run(context.Background(), RunInput{RunID: "run-hook-reason", AgentID: "agent-1", Instruction: "explore"})
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, RunCompleted, res.Status)
		assert.Eq(t, "max_tokens", res.WrapUp)
		assert.StrContains(t, res.Output, "本轮因 max_tokens 收尾")
	})

	t.Run("text of the reply is preferred over the notice", func(t *testing.T) {
		// Whitespace is not text: the reply below has no usable answer.
		_, provider, res, err := newRun(t, false, call(""), call(" \n "))
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, res.Output, "模型未给出汇报")
		assert.Eq(t, 2, len(provider.calls))
	})
}

// TestRuntime_Run_WrapUpNoticeWithoutText checks the notice of a wrap-up whose
// run never produced any text at all: it names the reason and omits the repeat.
func TestRuntime_Run_WrapUpNoticeWithoutText(t *testing.T) {
	store := newRuntimeTestStorage()
	provider := &runtimeTestProvider{results: []*GenerateResult{
		{ToolCalls: []ToolCall{{CallID: "call-1", Name: "lookup"}}, FinishReason: FinishToolCalls},
		{ToolCalls: []ToolCall{{CallID: "call-2", Name: "lookup"}}, FinishReason: FinishToolCalls},
	}}
	reg := tools.NewRegistry()
	reg.MustRegister(&runtimeTestTool{name: "lookup"})
	res, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          reg,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
		MaxSteps:       1,
		MaxStepsWrapUp: "WRAP UP",
	}).Run(context.Background(), RunInput{RunID: "run-no-text", AgentID: "agent-1", Instruction: "explore"})
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, RunCompleted, res.Status)
	assert.Eq(t, "max_steps", res.WrapUp)
	assert.StrContains(t, res.Output, "本轮因步数上限（1 步）收尾")
	assert.False(t, strings.Contains(res.Output, "最后一次有正文的回复"))
}
