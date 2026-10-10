package wiring

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/pkg/agentcore"
	agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"
)

// nativeStubTool is a native tool that records whether it ran.
type nativeStubTool struct {
	name string
	runs int
}

func (t *nativeStubTool) Name() string        { return t.name }
func (t *nativeStubTool) Description() string { return "native stub tool" }
func (t *nativeStubTool) Kind() tools.Kind    { return tools.KindNative }
func (t *nativeStubTool) Schema() agentcoreapi.ToolSchema {
	return agentcoreapi.ToolSchema{Name: t.name}
}
func (t *nativeStubTool) Execute(_ context.Context, call agentcoreapi.ToolCall) (*agentcoreapi.ToolResult, error) {
	t.runs++
	return &agentcoreapi.ToolResult{CallID: call.CallID, Name: call.Name, Content: "ran"}, nil
}

// scriptedAsker answers questions from a script and records them.
type scriptedAsker struct {
	answers   []string
	err       error
	questions []tools.Question
}

func (a *scriptedAsker) Ask(_ context.Context, q tools.Question) (string, error) {
	a.questions = append(a.questions, q)
	if a.err != nil {
		return "", a.err
	}
	if len(a.answers) == 0 {
		return "", nil
	}
	answer := a.answers[0]
	a.answers = a.answers[1:]
	return answer, nil
}

// newChatApproval builds the chat approval layer over stub tools.
func newChatApproval(t *testing.T, auto bool, asker tools.Asker) (*tools.Approval, map[string]*nativeStubTool) {
	t.Helper()
	registry := tools.NewRegistry()
	stubs := map[string]*nativeStubTool{}
	for _, name := range []string{"file_read", "file_list", "update_plan", "tool_search", "file_write", "shell_exec", "web_search", "file_grep", "file_find", "future_tool"} {
		stubs[name] = &nativeStubTool{name: name}
		registry.Register(stubs[name])
	}
	for _, name := range []string{"gofer_issue_list", "gofer_issue_close"} {
		registry.Register(newServerStubTool("gofer", name))
	}
	gate, err := newCLIMCPGate(false)
	assert.Require(t, assert.NoErr(t, err))
	return tools.NewApproval(registry, newChatApprover(gate, auto, asker, "/work")), stubs
}

func runTool(approval *tools.Approval, name string, args map[string]any) error {
	_, err := approval.ExecuteTool(context.Background(), agentcore.ToolCall{CallID: "c", Name: name, Arguments: args})
	return err
}

func TestChatApprover(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	assert.NoErr(t, gkconfig.Set("tools.auto_allow", []string{"gofer/gofer_*_list"}))

	t.Run("read-only tools run without asking", func(t *testing.T) {
		asker := &scriptedAsker{}
		approval, stubs := newChatApproval(t, false, asker)
		for _, name := range []string{"file_read", "file_list", "update_plan", "tool_search", "web_search", "file_grep", "file_find"} {
			assert.NoErr(t, runTool(approval, name, nil))
			assert.Eq(t, 1, stubs[name].runs)
		}
		assert.Len(t, asker.questions, 0)
	})

	t.Run("answers", func(t *testing.T) {
		for _, tc := range []struct {
			answer string
			allow  bool
		}{
			{"y", true}, {"yes", true}, {" Y ", true}, {"YES", true},
			{"n", false}, {"no", false}, {"", false}, {"ok", false}, {"yy", false},
		} {
			asker := &scriptedAsker{answers: []string{tc.answer}}
			approval, stubs := newChatApproval(t, false, asker)
			err := runTool(approval, "shell_exec", map[string]any{"command": "echo hi"})
			if tc.allow {
				assert.NoErr(t, err, tc.answer)
				assert.Eq(t, 1, stubs["shell_exec"].runs)
				continue
			}
			assert.ErrIs(t, err, tools.ErrDenied, tc.answer)
			assert.Eq(t, "用户拒绝执行 shell_exec", err.Error())
			assert.Eq(t, 0, stubs["shell_exec"].runs)
		}
	})

	t.Run("always allows the same tool for the process only", func(t *testing.T) {
		asker := &scriptedAsker{answers: []string{"a", "n"}}
		approval, stubs := newChatApproval(t, false, asker)
		assert.NoErr(t, runTool(approval, "shell_exec", map[string]any{"command": "ls"}))
		assert.NoErr(t, runTool(approval, "shell_exec", map[string]any{"command": "pwd"}))
		assert.Eq(t, 2, stubs["shell_exec"].runs)
		assert.Len(t, asker.questions, 1)
		// Another tool is still asked.
		assert.ErrIs(t, runTool(approval, "file_write", map[string]any{"path": "a", "content": "x"}), tools.ErrDenied)
		assert.Len(t, asker.questions, 2)
		// A new approver (a new chat process) asks again.
		asker2 := &scriptedAsker{answers: []string{"always"}}
		approval2, _ := newChatApproval(t, false, asker2)
		assert.NoErr(t, runTool(approval2, "shell_exec", map[string]any{"command": "ls"}))
		assert.Len(t, asker2.questions, 1)
	})

	t.Run("future tools are asked too", func(t *testing.T) {
		asker := &scriptedAsker{answers: []string{"n"}}
		approval, _ := newChatApproval(t, false, asker)
		assert.ErrIs(t, runTool(approval, "future_tool", map[string]any{"x": 1}), tools.ErrDenied)
		assert.Require(t, assert.Len(t, asker.questions, 1))
		assert.Eq(t, []string{`参数：{"x":1}`}, asker.questions[0].Details)
	})

	t.Run("auto runs without asking", func(t *testing.T) {
		asker := &scriptedAsker{}
		approval, stubs := newChatApproval(t, true, asker)
		for _, name := range []string{"shell_exec", "file_write", "web_search"} {
			assert.NoErr(t, runTool(approval, name, nil))
			assert.Eq(t, 1, stubs[name].runs)
		}
		assert.Len(t, asker.questions, 0)
	})

	t.Run("unbound asker refuses with the auto hint", func(t *testing.T) {
		approval, stubs := newChatApproval(t, false, tools.NewBindableAsker())
		err := runTool(approval, "shell_exec", map[string]any{"command": "ls"})
		assert.ErrIs(t, err, tools.ErrDenied)
		assert.StrContains(t, err.Error(), "tools.approval: auto")
		assert.Eq(t, 0, stubs["shell_exec"].runs)
	})

	t.Run("an asker error aborts the turn", func(t *testing.T) {
		for _, cause := range []error{io.EOF, context.Canceled} {
			approval, stubs := newChatApproval(t, false, &scriptedAsker{err: cause})
			err := runTool(approval, "shell_exec", map[string]any{"command": "ls"})
			assert.ErrIs(t, err, tools.ErrAborted)
			assert.ErrIs(t, err, cause)
			assert.False(t, errors.Is(err, tools.ErrDenied))
			assert.Eq(t, 0, stubs["shell_exec"].runs)
		}
	})

	t.Run("MCP rules are unchanged and never asked", func(t *testing.T) {
		asker := &scriptedAsker{answers: []string{"y", "y"}}
		approval, _ := newChatApproval(t, false, asker)
		assert.NoErr(t, runTool(approval, "gofer_issue_list", nil))
		err := runTool(approval, "gofer_issue_close", nil)
		assert.True(t, deniedCall(err, "gofer_issue_close"))
		assert.Len(t, asker.questions, 0)
	})
}

func TestChatApproverQuestion(t *testing.T) {
	approver := newChatApprover(nil, false, nil, filepath.FromSlash("/work"))
	t.Run("shell command is full and redacted", func(t *testing.T) {
		long := "echo start-" + strings.Repeat("x", 500) + "-end"
		q := approver.question(agentcore.ToolCall{Name: "shell_exec", Arguments: map[string]any{
			"command": long + " && curl -H 'Authorization: Bearer sk-secret-token' https://e.x",
		}})
		assert.Eq(t, "shell_exec", q.Tool)
		assert.Require(t, assert.Len(t, q.Details, 1))
		assert.StrContains(t, q.Details[0], "命令："+long) // never truncated
		assert.NotContains(t, q.Details[0], "sk-secret-token")
		assert.Eq(t, "允许执行 shell_exec？[y]是 [N]否 [a]本会话总是允许该工具：", q.Prompt)
	})
	t.Run("file_write shows the full path and the content size", func(t *testing.T) {
		q := approver.question(agentcore.ToolCall{Name: "file_write", Arguments: map[string]any{"path": "dir/a.txt", "content": "你好"}})
		assert.Eq(t, []string{"路径：" + filepath.Join(filepath.FromSlash("/work"), "dir/a.txt"), "内容：6 字节"}, q.Details)
	})
	t.Run("web_search shows the query", func(t *testing.T) {
		q := approver.question(agentcore.ToolCall{Name: "web_search", Arguments: map[string]any{"query": "golang tcflush"}})
		assert.Eq(t, []string{"查询：golang tcflush"}, q.Details)
	})
	t.Run("control characters cannot hide part of a command", func(t *testing.T) {
		q := approver.question(agentcore.ToolCall{Name: "shell_exec", Arguments: map[string]any{"command": "rm -rf ~\r\x1b[2Kecho safe\nls\t-l"}})
		assert.Eq(t, "命令：rm -rf ~\\u000d\\u001b[2Kecho safe\nls\t-l", q.Details[0])
	})
}

func TestToolsApprovalConfig(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	for _, tc := range []struct {
		name  string
		value any
		auto  bool
		err   bool
	}{
		{name: "unset is ask", value: nil},
		{name: "ask", value: "ask"},
		{name: "auto", value: "auto", auto: true},
		{name: "unknown word", value: "always", err: true},
		{name: "upper case", value: "AUTO", err: true},
		{name: "empty", value: "", err: true},
		{name: "boolean", value: true, err: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gkconfig.Reset()
			if tc.value != nil {
				assert.NoErr(t, gkconfig.Set("tools.approval", tc.value))
			}
			auto, err := toolsApprovalAuto()
			if tc.err {
				assert.Err(t, err)
				assert.StrContains(t, err.Error(), `tools.approval must be "ask" or "auto"`)
				return
			}
			assert.NoErr(t, err)
			assert.Eq(t, tc.auto, auto)
		})
	}
}

// TestOpenCLIChatApproval checks the runtime wiring: chat gets an (unbound)
// asker and rejects an invalid tools.approval at startup; run has no asker.
func TestOpenCLIChatApproval(t *testing.T) {
	resetConfig(t)
	setProviders(t, map[string]any{"openai": map[string]any{"api_key": "k"}})
	assert.NoErr(t, gkconfig.Set("model", "openai/gpt-4o"))

	t.Run("chat exposes an unbound asker", func(t *testing.T) {
		rt, err := OpenCLI(context.Background(), Options{ChatMode: true})
		assert.Require(t, assert.NoErr(t, err))
		t.Cleanup(func() { _ = rt.Close() })
		assert.NotNil(t, rt.Asker)
		_, err = rt.Asker.Ask(context.Background(), tools.Question{})
		assert.ErrIs(t, err, tools.ErrCannotAsk)
	})
	t.Run("run has no asker", func(t *testing.T) {
		rt, err := OpenCLI(context.Background(), Options{})
		assert.Require(t, assert.NoErr(t, err))
		t.Cleanup(func() { _ = rt.Close() })
		assert.Nil(t, rt.Asker)
	})
	t.Run("invalid tools.approval fails chat startup", func(t *testing.T) {
		assert.NoErr(t, gkconfig.Set("tools.approval", "sometimes"))
		_, err := OpenCLI(context.Background(), Options{ChatMode: true})
		assert.Err(t, err)
		assert.StrContains(t, err.Error(), "tools.approval")
	})
}
