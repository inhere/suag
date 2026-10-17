package wiring

import (
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/tools/native"
)

// Lives here, not in pkg/agentcore: the kernel must not depend on tools/native
// (layering_test.go counts test imports too), while wiring owns both.
// The schemas of the native write tools take part: each names the field the
// model has to fix, and the empty placeholders of a strict-schema model pass.
func TestNativeWriteToolSchemasValidateArguments(t *testing.T) {
	env := native.Env{Workspace: t.TempDir(), TrashDir: t.TempDir()}
	schemas := make(map[string]agentcore.ToolSchema)
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
			calls := []agentcore.ToolCall{{Name: tc.tool, Arguments: tc.args}}
			agentcore.PrepareToolCalls([]agentcore.ToolSchema{schema}, calls)
			assert.StrContains(t, calls[0].ArgumentsError, tc.want)
		})
	}

	t.Run("a strict-schema model's empty placeholders pass", func(t *testing.T) {
		calls := []agentcore.ToolCall{{Name: "file_edit", Arguments: map[string]any{
			"path": "a.go", "old_string": "a", "new_string": "b", "edits": []any{}, "replace_all": false,
		}}}
		agentcore.PrepareToolCalls([]agentcore.ToolSchema{schemas["file_edit"]}, calls)
		assert.Eq(t, "", calls[0].ArgumentsError)
		_, hasEdits := calls[0].Arguments["edits"]
		assert.False(t, hasEdits)
	})

	t.Run("a fitting call is left untouched", func(t *testing.T) {
		args := map[string]any{"path": "a.go", "content": "x", "edits": []any{}}
		calls := []agentcore.ToolCall{{Name: "file_write", Arguments: args}}
		agentcore.PrepareToolCalls([]agentcore.ToolSchema{schemas["file_write"]}, calls)
		assert.Eq(t, "", calls[0].ArgumentsError)
	})
}
