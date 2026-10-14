package wiring

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/tools"
	"github.com/inhere/suga/pkg/agentcore/tools/native"
)

func TestDangerGuard(t *testing.T) {
	ws := t.TempDir()
	shell := native.NewShellExecTool(ws, 0)
	call := func(command string) agentcore.ToolCall {
		return agentcore.ToolCall{CallID: "c1", Name: "shell_exec", Arguments: map[string]any{"command": command}}
	}
	// A delete of ~ in the OS default shell (PowerShell on Windows).
	danger := "rm -rf ~"
	if runtime.GOOS == "windows" {
		danger = "Remove-Item -Recurse -Force ~"
	}
	innerRuns := 0
	inner := tools.ApproverFunc(func(context.Context, agentcore.ToolCall, agentcore.Tool) error {
		innerRuns++
		return nil
	})

	t.Run("ordinary commands go to the approver", func(t *testing.T) {
		innerRuns = 0
		assert.NoErr(t, guardDangerous(inner).Decide(context.Background(), call("go test ./..."), shell))
		assert.Eq(t, 1, innerRuns)
		assert.Nil(t, guardDangerous(nil)) // no approver stays deny-all
	})
	t.Run("run and ACP refuse, with the reason in the trace", func(t *testing.T) {
		innerRuns = 0
		ctx, report := tools.WithApprovalReport(context.Background())
		err := guardDangerous(inner).Decide(ctx, call(danger), shell)
		assert.True(t, errors.Is(err, tools.ErrDenied))
		assert.ErrMsgContains(t, err, "已拒绝危险删除："+danger+"：删除用户主目录")
		assert.ErrMsgContains(t, err, "file_remove")
		assert.Eq(t, 0, innerRuns)
		assert.StrContains(t, report.Danger, "用户主目录")
	})
	t.Run("chat asks even with approval auto; only yes runs", func(t *testing.T) {
		asker := &scriptedAsker{answers: []string{"y", "YES"}}
		guard := guardDangerous(testChatApprover(nil, true, asker, ws, nil))
		ctx, report := tools.WithApprovalReport(context.Background())
		err := guard.Decide(ctx, call(danger), shell)
		assert.ErrMsgContains(t, err, "用户拒绝执行危险删除") // "y" is not enough
		assert.Eq(t, tools.AskedAndDenied, report.Source)
		ctx, report = tools.WithApprovalReport(context.Background())
		assert.NoErr(t, guard.Decide(ctx, call(danger), shell))
		assert.Eq(t, tools.ApprovedByUser, report.Source)
		assert.Require(t, assert.Len(t, asker.questions, 2))
		assert.StrContains(t, asker.questions[0].Details[1], "⚠ 危险删除："+danger+"：删除用户主目录")
		assert.StrContains(t, asker.questions[0].Prompt, "输入 yes 执行")
	})
	t.Run("chat without a terminal refuses", func(t *testing.T) {
		guard := guardDangerous(testChatApprover(nil, false, &scriptedAsker{err: tools.ErrCannotAsk}, ws, nil))
		err := guard.Decide(context.Background(), call(danger), shell)
		assert.ErrMsgContains(t, err, "无法向用户确认")
	})
}
