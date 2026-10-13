package native

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/pkg/agentcore/api"
	"github.com/inhere/suga/pkg/agentcore/llm"
	"github.com/inhere/suga/pkg/agentcore/tasks"
)

func TestBackgroundShellAndTaskTool(t *testing.T) {
	dir := t.TempDir()
	m := tasks.NewManager(tasks.Config{Output: func(sid, id string) (string, string, error) {
		return filepath.Join(dir, id+".txt"), "out:" + sid + ":" + id, nil
	}})
	t.Cleanup(m.Close)
	ctx := llm.WithSessionID(context.Background(), "s1")
	shell := NewShellExecTool(t.TempDir(), 0)
	shell.SetTasks(m)
	task := NewTaskTool(m, 0)
	quick, slow := "echo hello token=sk-abc123def456ghi789; echo done", "sleep 30"
	if runtime.GOOS == "windows" {
		quick, slow = "Write-Output 'hello token=sk-abc123def456ghi789'; Write-Output done", "Start-Sleep -Seconds 30"
	}
	run := func(tool api.Tool, args map[string]any) (*api.ToolResult, error) {
		return tool.Execute(ctx, api.ToolCall{CallID: "c", Name: tool.Name(), Arguments: args})
	}

	t.Run("schema offers background only with a manager", func(t *testing.T) {
		props := shell.Schema().InputSchema["properties"].(map[string]any)
		assert.NotNil(t, props["background"])
		plain := NewShellExecTool(t.TempDir(), 0).Schema().InputSchema["properties"].(map[string]any)
		assert.Nil(t, plain["background"])
	})
	t.Run("start, wait, read", func(t *testing.T) {
		res, err := run(shell, map[string]any{"command": quick, "background": true})
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, res.Content, "[background task t1 started]")
		assert.Eq(t, "t1", res.Metadata["task_id"])
		res, err = run(task, map[string]any{"action": "wait", "id": "t1", "timeout": 20})
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, res.Content, "t1 exited (exit 0)")
		assert.StrContains(t, res.Content, "[last lines]\nhello token=[REDACTED]\ndone") // secrets masked
		res, err = run(task, map[string]any{"action": "output", "id": "t1", "grep": "done"})
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, res.Content, "[1 lines match \"done\"; showing 1]\n2: done")
		res, err = run(task, map[string]any{"action": "output", "id": "t1", "offset": 1, "limit": 1})
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, res.Content, "[lines 1-1 of 2]")
		assert.StrContains(t, res.Content, "[more: offset 2]")
	})
	t.Run("wait times out, kill stops it, list shows both", func(t *testing.T) {
		_, err := run(shell, map[string]any{"command": slow, "background": true})
		assert.Require(t, assert.NoErr(t, err))
		res, err := run(task, map[string]any{"action": "wait", "id": "t2", "timeout": 1})
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, res.Content, "[still running after waiting 1s]")
		res, err = run(task, map[string]any{"action": "kill", "id": "t2"})
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, res.Content, "t2 killed after")
		res, err = run(task, map[string]any{"action": "list"})
		assert.Require(t, assert.NoErr(t, err))
		assert.StrContains(t, res.Content, "t1 exited")
		assert.StrContains(t, res.Content, "t2 killed")
	})
	t.Run("errors", func(t *testing.T) {
		_, err := run(task, map[string]any{"action": "wait", "id": "t9"})
		assert.ErrMsgContains(t, err, "unknown task")
		_, err = run(task, map[string]any{"action": "pause"})
		assert.ErrMsgContains(t, err, "action must be")
	})
}
