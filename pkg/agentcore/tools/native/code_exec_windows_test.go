//go:build windows

package native

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
)

func TestShellExecWindows(t *testing.T) {
	tool := NewShellExecTool(t.TempDir(), 0)
	assert.True(t, tool.shell.powershell)

	t.Run("Chinese output is not mangled", func(t *testing.T) {
		res, _ := runShell(t, tool, map[string]any{"command": `Write-Output "中文输出"; cmd /c echo 原生中文`})
		assert.StrContains(t, res.Content, "中文输出")
		assert.StrContains(t, res.Content, "原生中文")
	})
	t.Run("double quotes survive", func(t *testing.T) {
		res, _ := runShell(t, tool, map[string]any{"command": `Write-Output "a ""b"" c"; echo 'single'`})
		assert.StrContains(t, res.Content, `a "b" c`)
		assert.StrContains(t, res.Content, "single")
	})
	t.Run("native exit code", func(t *testing.T) {
		res, _ := runShell(t, tool, map[string]any{"command": "cmd /c exit 3"})
		assert.StrContains(t, res.Content, "[exit code: 3]")
	})
	t.Run("failed cmdlet exits 1", func(t *testing.T) {
		res, _ := runShell(t, tool, map[string]any{"command": "Get-Item does-not-exist-xyz"})
		assert.StrContains(t, res.Content, "[exit code: 1]")
	})
	t.Run("success exits 0", func(t *testing.T) {
		res, _ := runShell(t, tool, map[string]any{"command": "Write-Output ok"})
		assert.StrContains(t, res.Content, "ok\n[exit code: 0]")
	})
	t.Run("timeout keeps partial output", func(t *testing.T) {
		res, took := runShell(t, tool, map[string]any{"command": "Write-Output before; Start-Sleep 30", "timeout": 2})
		assert.StrContains(t, res.Content, "before")
		assert.StrContains(t, res.Content, "[timeout after 2s")
		assert.True(t, took < 6*time.Second, "took %s", took)
	})
	t.Run("timeout kills child processes", func(t *testing.T) {
		marker := "127.0.0.1"
		res, took := runShell(t, tool, map[string]any{"command": "ping -n 60 " + marker, "timeout": 2})
		assert.StrContains(t, res.Content, "[timeout after 2s")
		assert.True(t, took < 6*time.Second, "took %s", took)
		time.Sleep(500 * time.Millisecond)
		out, _ := exec.Command("tasklist", "/FI", "IMAGENAME eq PING.EXE", "/FO", "CSV", "/NH").Output()
		assert.False(t, strings.Contains(strings.ToUpper(string(out)), "PING.EXE"), "ping still running: %s", out)
	})
}
