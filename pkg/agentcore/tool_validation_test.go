package agentcore

import (
	"context"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/pkg/agentcore/tools"
	"github.com/inhere/suga/pkg/agentcore/tools/native"
)

// testToolSchema is the schema the validator cases below check, and mirrors
// what the native write tools declare (type, enum, range, nested items).
func testToolSchema() ToolSchema {
	return ToolSchema{Name: "file_write", InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
			"mode":    map[string]any{"type": "string", "enum": []any{"create", "replace"}},
			"chunk":   map[string]any{"type": "integer", "minimum": float64(1), "maximum": float64(10)},
			"tags": map[string]any{"type": "array", "minItems": float64(1), "maxItems": float64(2),
				"items": map[string]any{"type": "string"}},
			"edits": map[string]any{"type": "array", "items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"old_string": map[string]any{"type": "string"},
					"new_string": map[string]any{"type": "string"},
				},
				"required": []any{"old_string", "new_string"},
			}},
			"opts": map[string]any{"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "number"}}},
			"free": map[string]any{"description": "no declared type"},
		},
		"required": []any{"path", "content"},
	}}
}

func TestValidateToolCallArguments(t *testing.T) {
	schema := testToolSchema()
	cases := map[string]struct {
		args map[string]any
		want []string
	}{
		"a fitting call": {
			args: map[string]any{"path": "a.go", "content": "x", "chunk": float64(3), "mode": "replace"},
		},
		"missing and blank required fields are named": {
			args: map[string]any{"path": "  "},
			want: []string{`missing required argument "path"`, `missing required argument "content"`},
		},
		"a wrong property type names field and types": {
			args: map[string]any{"path": "a.go", "content": float64(12)},
			want: []string{`argument "content" expects string, got integer`},
		},
		"a wrong enum names the allowed values": {
			args: map[string]any{"path": "a.go", "content": "x", "mode": "overwrite"},
			want: []string{`argument "mode" must be one of "create", "replace", got "overwrite"`},
		},
		"range and size limits are reported": {
			args: map[string]any{"path": "a.go", "content": "x", "chunk": float64(0), "tags": []any{"a", "b", "c"}},
			want: []string{`argument "chunk" must be at least 1, got 0`, `argument "tags" takes at most 2 item(s), got 3`},
		},
		"a bad array item is named by index": {
			args: map[string]any{"path": "a.go", "content": "x", "tags": []any{float64(7)}},
			want: []string{`argument "tags[0]" expects string, got integer`},
		},
		"a nested object names the field inside the item": {
			args: map[string]any{"path": "a.go", "content": "x",
				"edits": []any{map[string]any{"old_string": float64(5)}}},
			want: []string{`missing required argument "edits[0].new_string"`,
				`argument "edits[0].old_string" expects string, got integer`},
		},
		"a blank nested required field is a real value": {
			args: map[string]any{"path": "a.go", "content": "x",
				"edits": []any{map[string]any{"old_string": "x", "new_string": ""}}},
		},
		"a union schema is left alone": {
			args: map[string]any{"path": "a.go", "content": "x", "opts": []any{"nope"}},
		},
		"a property without a declared type is not guessed at": {
			args: map[string]any{"path": "a.go", "content": "x", "free": map[string]any{"nested": true}},
		},
		"a field the schema does not mention passes through": {
			args: map[string]any{"path": "a.go", "content": "x", "extra": "7"},
		},
		"a null optional value is not a type error": {
			args: map[string]any{"path": "a.go", "content": "x", "mode": nil},
		},
		"a string length limit is reported": {
			args: map[string]any{"path": "a.go", "content": "x"},
			want: nil, // minLength is not declared for these fields: nothing to report
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Eq(t, tc.want, validateToolCallArguments(schema, ToolCall{Name: "file_write", Arguments: tc.args}))
		})
	}
}

func TestValidateToolCallArgumentsRejectsUnknownFields(t *testing.T) {
	schema := ToolSchema{Name: "strict", InputSchema: map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"path": map[string]any{"type": "string"}},
		"required":             []any{"path"},
		"additionalProperties": false,
	}}
	t.Run("additionalProperties false names the accepted fields", func(t *testing.T) {
		issues := validateToolCallArguments(schema, ToolCall{Name: "strict",
			Arguments: map[string]any{"path": "a.go", "content": "x"}})
		assert.Eq(t, []string{`unknown argument "content": expected only "path"`}, issues)
	})
	t.Run("additionalProperties true stays permissive", func(t *testing.T) {
		permissive := ToolSchema{Name: "strict", InputSchema: map[string]any{
			"type":                 "object",
			"properties":           map[string]any{"path": map[string]any{"type": "string"}},
			"additionalProperties": true,
		}}
		assert.Empty(t, validateToolCallArguments(permissive, ToolCall{Name: "strict",
			Arguments: map[string]any{"path": "a.go", "content": "x"}}))
	})
}

// Models with strict tool schemas fill every optional field: an empty
// placeholder counts as not given (the tool then reads its own default), while
// a blank required value still counts as missing.
func TestCoerceArgumentsDropsOptionalPlaceholders(t *testing.T) {
	schema := testToolSchema()

	t.Run("optional placeholders are dropped", func(t *testing.T) {
		args := map[string]any{"path": "a.go", "content": "x",
			"mode": "", "tags": []any{}, "edits": []any{}, "free": map[string]any{}}
		coerceArguments(schema, args)
		assert.Eq(t, map[string]any{"path": "a.go", "content": "x"}, args)
	})

	t.Run("a blank required value stays for the validator", func(t *testing.T) {
		args := map[string]any{"path": "a.go", "content": "  "}
		coerceArguments(schema, args)
		assert.Eq(t, map[string]any{"path": "a.go", "content": "  "}, args)
		assert.Eq(t, []string{`missing required argument "content"`},
			validateToolCallArguments(schema, ToolCall{Name: "file_write", Arguments: args}))
	})
}

// lastToolCallMessage returns the last tool result message of a run, so a test
// can read what the model was told about its call.
func lastToolCallMessage(store *runtimeTestStorage, runID string) *Message {
	var found *Message
	for i, msg := range store.messages[runID] {
		if msg.Role == "tool" {
			found = &store.messages[runID][i]
		}
	}
	return found
}

// A call whose arguments do not fit the tool schema is not run: the model gets
// one error result naming every problem, and the run goes on.
func TestRuntime_Run_AnswersToolArgumentsThatDoNotFitTheSchema(t *testing.T) {
	store := newRuntimeTestStorage()
	reg := tools.NewRegistry()
	tool := &runtimeTestToolWithSchema{
		runtimeTestTool: runtimeTestTool{name: "file_write"},
		inputSchema:     testToolSchema().InputSchema,
	}
	reg.MustRegister(tool)
	provider := &runtimeTestProvider{results: []*GenerateResult{
		{FinishReason: FinishToolCalls, ToolCalls: []ToolCall{{CallID: "c1", Name: "file_write",
			Arguments: map[string]any{"content": map[string]any{"text": "x"}, "mode": "overwrite"}}}},
		{Content: "fixed", FinishReason: FinishFinal},
	}}

	result, err := New(Config{
		Provider:       provider,
		Storage:        store,
		Tools:          reg,
		PromptBuilder:  runtimeTestPromptBuilder{},
		ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{RunID: "run-schema-args", AgentID: "agent-1", Instruction: "write"})
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, "fixed", result.Output)
	assert.Empty(t, tool.calls) // the tool never ran

	msg := lastToolCallMessage(store, "run-schema-args")
	assert.Require(t, assert.NotNil(t, msg))
	assert.True(t, msg.ToolResult.IsError)
	assert.StrContains(t, msg.Content, ErrToolArgumentsInvalid.Error())
	assert.StrContains(t, msg.Content, `missing required argument "path"`)
	assert.StrContains(t, msg.Content, `argument "content" expects string, got object`)
	assert.StrContains(t, msg.Content, `argument "mode" must be one of "create", "replace", got "overwrite"`)
	assert.Eq(t, RunCompleted, store.runs["run-schema-args"].Status)
}

// The dropped placeholders and the repaired values are what the tool receives.
func TestRuntime_Run_DropsOptionalPlaceholdersBeforeRunning(t *testing.T) {
	store := newRuntimeTestStorage()
	reg := tools.NewRegistry()
	tool := &runtimeTestToolWithSchema{
		runtimeTestTool: runtimeTestTool{name: "file_write", result: &ToolResult{Content: "written"}},
		inputSchema:     testToolSchema().InputSchema,
	}
	reg.MustRegister(tool)
	provider := &runtimeTestProvider{results: []*GenerateResult{
		{FinishReason: FinishToolCalls, ToolCalls: []ToolCall{{CallID: "c1", Name: "file_write",
			Arguments: map[string]any{"path": "a.go", "content": "x", "edits": []any{}, "mode": "", "chunk": "4"}}}},
		{Content: "done", FinishReason: FinishFinal},
	}}

	_, err := New(Config{Provider: provider, Storage: store, Tools: reg,
		PromptBuilder: runtimeTestPromptBuilder{}, ContextBuilder: runtimeTestContextBuilder{},
	}).Run(context.Background(), RunInput{RunID: "run-placeholders", AgentID: "a", Instruction: "write"})
	assert.Require(t, assert.NoErr(t, err))
	assert.Require(t, assert.Eq(t, 1, len(tool.calls)))
	assert.Eq(t, float64(4), tool.calls[0].Arguments["chunk"])
	_, hasEdits := tool.calls[0].Arguments["edits"]
	assert.False(t, hasEdits)
	_, hasMode := tool.calls[0].Arguments["mode"]
	assert.False(t, hasMode)
}

// The schemas of the native write tools take part: each names the field the
// model has to fix, and the empty placeholders of a strict-schema model pass.
func TestPrepareToolCallsValidatesBuiltinWriteToolSchemas(t *testing.T) {
	env := native.Env{Workspace: t.TempDir(), TrashDir: t.TempDir()}
	schemas := make(map[string]ToolSchema)
	for _, tool := range native.BuildTools(env, true) {
		schemas[tool.Name()] = tool.Schema()
	}
	schemas["task"] = native.NewTaskTool(nil, 0).Schema()

	cases := map[string]struct {
		tool string
		args map[string]any
		want string
	}{
		"file_write names the field and the type": {
			tool: "file_write", args: map[string]any{"path": "a.go", "content": float64(12)},
			want: `argument "content" expects string, got integer`,
		},
		"file_edit names the field inside the item": {
			tool: "file_edit", args: map[string]any{"path": "a.go", "edits": []any{map[string]any{"old_string": float64(5)}}},
			want: `argument "edits[0].old_string" expects string, got integer`,
		},
		"shell_exec names the field": {
			tool: "shell_exec", args: map[string]any{"command": []any{"go", "test"}},
			want: `argument "command" expects string, got array`,
		},
		"task names the enum values": {
			tool: "task", args: map[string]any{"action": "run"},
			want: `argument "action" must be one of "list", "output", "wait", "kill", got "run"`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			schema, ok := schemas[tc.tool]
			assert.True(t, ok, "the tool is in the catalog: "+tc.tool)
			calls := []ToolCall{{Name: tc.tool, Arguments: tc.args}}
			prepareToolCalls([]ToolSchema{schema}, calls)
			assert.StrContains(t, calls[0].ArgumentsError, tc.want)
		})
	}

	t.Run("a strict-schema model's empty placeholders pass", func(t *testing.T) {
		calls := []ToolCall{{Name: "file_edit", Arguments: map[string]any{
			"path": "a.go", "old_string": "a", "new_string": "b", "edits": []any{}, "replace_all": false,
		}}}
		prepareToolCalls([]ToolSchema{schemas["file_edit"]}, calls)
		assert.Eq(t, "", calls[0].ArgumentsError)
		_, hasEdits := calls[0].Arguments["edits"]
		assert.False(t, hasEdits)
	})

	t.Run("a fitting call is left untouched", func(t *testing.T) {
		args := map[string]any{"path": "a.go", "content": "x", "edits": []any{}}
		calls := []ToolCall{{Name: "file_write", Arguments: args}}
		prepareToolCalls([]ToolSchema{schemas["file_write"]}, calls)
		assert.Eq(t, "", calls[0].ArgumentsError)
	})
}
