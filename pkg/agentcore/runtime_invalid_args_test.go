package agentcore

import (
	"context"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suag/pkg/agentcore/tools"
)

func TestRuntime_Run_AnswersUndecodableToolArguments(t *testing.T) {
	store := newRuntimeTestStorage()
	reg := tools.NewRegistry()
	tool := &runtimeTestToolWithSchema{
		runtimeTestTool: runtimeTestTool{name: "file_edit"},
		inputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
			"required":   []any{"path"},
		},
	}
	reg.MustRegister(tool)
	provider := &runtimeTestProvider{results: []*GenerateResult{
		{
			ToolCalls: []ToolCall{{CallID: "call-1", Name: "file_edit", Arguments: map[string]any{},
				ArgumentsError: `decode tool call "file_edit" arguments: invalid character '\n' in string literal`}},
			FinishReason: FinishToolCalls,
		},
		{Content: "resent", FinishReason: FinishFinal},
	}}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          reg,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{RunID: "run-bad-args", AgentID: "agent-1", Instruction: "edit"})
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, "resent", result.Output)
	assert.Empty(t, tool.calls) // the tool never ran

	msgs := store.messages["run-bad-args"]
	var toolMsg *Message
	for i := range msgs {
		if msgs[i].Role == "tool" {
			toolMsg = &msgs[i]
		}
	}
	assert.Require(t, assert.NotNil(t, toolMsg))
	assert.True(t, toolMsg.ToolResult.IsError)
	assert.StrContains(t, toolMsg.Content, "are not valid JSON")
	assert.StrContains(t, toolMsg.Content, "invalid character")
	// The model saw the error on its next request.
	assert.Eq(t, 2, len(provider.calls))
	assert.Eq(t, RunCompleted, store.runs["run-bad-args"].Status)
}
