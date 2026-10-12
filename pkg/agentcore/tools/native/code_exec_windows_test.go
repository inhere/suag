//go:build windows

package native

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
)

func TestShellExecWindows(t *testing.T) {
	tool := NewShellExecTool(t.TempDir(), 0)
	assert.True(t, tool.shell.powershell)

	t.Run("colour codes are removed", func(t *testing.T) {
		res, _ := runShell(t, tool, map[string]any{"command": "Write-Output \"$([char]27)[32mTest Name$([char]27)[0m: x\""})
		assert.StrContains(t, res.Content, "Test Name: x")
		assert.NotContains(t, res.Content, "\x1b")
	})
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
	t.Run("table output is not dropped", func(t *testing.T) {
		res, _ := runShell(t, tool, map[string]any{"command": `Get-ChildItem $env:WINDIR | Select-Object -First 2 Name`})
		assert.StrContains(t, res.Content, "Name")
		assert.StrContains(t, res.Content, "----")
		assert.StrContains(t, res.Content, "[exit code: 0]")
		assert.StrNotContains(t, res.Content, "\x1b[")
	})
	t.Run("exit inside the command", func(t *testing.T) {
		res, _ := runShell(t, tool, map[string]any{"command": "$x = 5; $x * 2; exit 7"})
		assert.StrContains(t, res.Content, "10\n[exit code: 7]")
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
		// Check this run's own child by PID: other test runs on the machine
		// (parallel evals) may have a ping of their own running.
		res, took := runShell(t, tool, map[string]any{"command": "$p = Start-Process -FilePath ping.exe " +
			"-ArgumentList '-n','60','127.0.0.1' -NoNewWindow -PassThru; Write-Output \"child=$($p.Id)\"; $p.WaitForExit()", "timeout": 2})
		assert.StrContains(t, res.Content, "[timeout after 2s")
		assert.True(t, took < 6*time.Second, "took %s", took)
		m := regexp.MustCompile(`child=(\d+)`).FindStringSubmatch(res.Content)
		assert.Require(t, assert.NotNil(t, m, "no child pid in %q", res.Content))
		time.Sleep(500 * time.Millisecond)
		out, _ := exec.Command("tasklist", "/FI", "PID eq "+m[1], "/FO", "CSV", "/NH").Output()
		assert.False(t, strings.Contains(strings.ToUpper(string(out)), "PING.EXE"), "ping %s still running: %s", m[1], out)
	})
}
