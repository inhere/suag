package agentcore

import (
	"context"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/pkg/agentcore/tools"
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
	assert.StrContains(t, toolMsg.Content, "this file_edit call was not run")
	assert.StrContains(t, toolMsg.Content, "invalid character")
	// The model saw the error on its next request.
	assert.Eq(t, 2, len(provider.calls))
	assert.Eq(t, RunCompleted, store.runs["run-bad-args"].Status)
}

func TestCoerceArguments(t *testing.T) {
	schema := ToolSchema{Name: "t", InputSchema: map[string]any{"properties": map[string]any{
		"paths":  map[string]any{"type": "array"},
		"opts":   map[string]any{"type": "object"},
		"limit":  map[string]any{"type": "integer"},
		"ratio":  map[string]any{"type": "number"},
		"all":    map[string]any{"type": "boolean"},
		"name":   map[string]any{"type": "string"},
		"broken": map[string]any{"type": "array"},
	}}}
	args := map[string]any{
		"paths": `["a.go", "b.go"]`, "opts": `{"x": 1}`, "limit": " 40 ", "ratio": "0.5", "all": "true",
		"name": "[not decoded]", "broken": "[1,", "gone": nil, "extra": "7",
	}
	coerceArguments(schema, args)
	assert.Eq(t, []any{"a.go", "b.go"}, args["paths"])
	assert.Eq(t, map[string]any{"x": float64(1)}, args["opts"])
	assert.Eq(t, float64(40), args["limit"])
	assert.Eq(t, 0.5, args["ratio"])
	assert.Eq(t, true, args["all"])
	assert.Eq(t, "[not decoded]", args["name"]) // a string property stays a string
	assert.Eq(t, "[1,", args["broken"])         // undecodable: left for the tool to report
	_, present := args["gone"]
	assert.False(t, present)
	assert.Eq(t, "7", args["extra"]) // no schema type: untouched
}

// The repaired arguments are what the tool receives.
func TestRuntime_Run_CoercesArgumentsBeforeRunning(t *testing.T) {
	store := newRuntimeTestStorage()
	reg := tools.NewRegistry()
	tool := &runtimeTestToolWithSchema{
		runtimeTestTool: runtimeTestTool{name: "file_read", result: &ToolResult{Content: "ok"}},
		inputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"paths": map[string]any{"type": "array"}, "limit": map[string]any{"type": "integer"},
		}},
	}
	reg.MustRegister(tool)
	provider := &runtimeTestProvider{results: []*GenerateResult{
		{ToolCalls: []ToolCall{{CallID: "c1", Name: "file_read", Arguments: map[string]any{"paths": `["a.go"]`, "limit": "20"}}}, FinishReason: FinishToolCalls},
		{Content: "done", FinishReason: FinishFinal},
	}}
	_, err := New(Config{Provider: provider, Storage: store, Tools: reg,
		PromptBuilder: runtimeTestPromptBuilder{}, ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{RunID: "run-coerce", AgentID: "a", Instruction: "read"})
	assert.Require(t, assert.NoErr(t, err))
	assert.Require(t, assert.Eq(t, 1, len(tool.calls)))
	assert.Eq(t, []any{"a.go"}, tool.calls[0].Arguments["paths"])
	assert.Eq(t, float64(20), tool.calls[0].Arguments["limit"])
}
