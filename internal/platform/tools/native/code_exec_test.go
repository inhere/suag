package native

import (
	"context"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suag/pkg/agentcore/api"
)

func runShell(t *testing.T, tool *ShellExecTool, args map[string]any) (*api.ToolResult, time.Duration) {
	t.Helper()
	start := time.Now()
	res, err := tool.Execute(context.Background(), api.ToolCall{CallID: "c", Name: "shell_exec", Arguments: args})
	assert.Require(t, assert.NoErr(t, err))
	return res, time.Since(start)
}

func TestShellExecCallTimeout(t *testing.T) {
	tool := NewShellExecTool(t.TempDir(), 0)
	tool.maxTimeout = 10 * time.Second
	t.Run("default", func(t *testing.T) {
		assert.Eq(t, DefaultShellTimeout, tool.callTimeout(map[string]any{}))
	})
	t.Run("number and string", func(t *testing.T) {
		assert.Eq(t, 3*time.Second, tool.callTimeout(map[string]any{"timeout": float64(3)}))
		assert.Eq(t, 4*time.Second, tool.callTimeout(map[string]any{"timeout": "4"}))
	})
	t.Run("clamped to max", func(t *testing.T) {
		assert.Eq(t, 10*time.Second, tool.callTimeout(map[string]any{"timeout": float64(9999)}))
	})
}

func TestShellExecRequiresCommand(t *testing.T) {
	_, err := NewShellExecTool(t.TempDir(), 0).Execute(context.Background(), api.ToolCall{Arguments: map[string]any{"command": "  "}})
	assert.ErrMsgContains(t, err, "command is required")
}

func TestDecodeOutputFallsBackToGBK(t *testing.T) {
	gbk := []byte{0xd6, 0xd0, 0xce, 0xc4} // "中文" in GBK
	assert.Eq(t, "中文", decodeOutput(gbk))
	assert.Eq(t, "中文", decodeOutput([]byte("中文")))
}

func TestResolveShellExplicit(t *testing.T) {
	assert.True(t, resolveShell(`C:\Program Files\PowerShell\7\pwsh.exe`).powershell)
	assert.False(t, resolveShell("bash").powershell)
	assert.Eq(t, []string{"bash", "-c", "ls"}, resolveShell("bash").argv("ls"))
}

func TestShellExecOutputIsCapped(t *testing.T) {
	tool := NewShellExecTool(t.TempDir(), 0)
	tool.maxOut = 200
	cmd := "for i in $(seq 1 200); do echo line$i; done"
	if tool.shell.powershell {
		cmd = "1..200 | ForEach-Object { \"line$_\" }"
	}
	res, _ := runShell(t, tool, map[string]any{"command": cmd})
	assert.StrContains(t, res.Content, "line1\n")
	assert.StrContains(t, res.Content, "line200")
	assert.StrContains(t, res.Content, "省略")
	assert.StrContains(t, res.Content, "[exit code: 0]")
}
