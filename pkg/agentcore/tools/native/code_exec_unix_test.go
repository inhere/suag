//go:build !windows

package native

import (
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
)

func TestShellExecUnix(t *testing.T) {
	tool := NewShellExecTool(t.TempDir(), 0)

	t.Run("exit code and stderr are returned", func(t *testing.T) {
		res, _ := runShell(t, tool, map[string]any{"command": "echo out; echo err >&2; exit 3"})
		assert.StrContains(t, res.Content, "out\n[stderr]\nerr")
		assert.StrContains(t, res.Content, "[exit code: 3]")
		assert.Eq(t, 3, res.Metadata["exit_code"])
	})
	t.Run("timeout keeps partial output", func(t *testing.T) {
		res, took := runShell(t, tool, map[string]any{"command": "echo before; sleep 30", "timeout": 1})
		assert.StrContains(t, res.Content, "before")
		assert.StrContains(t, res.Content, "[timeout after 1s")
		assert.True(t, took < 4*time.Second, "took %s", took)
	})
	t.Run("background child holding the pipe does not hang", func(t *testing.T) {
		res, took := runShell(t, tool, map[string]any{"command": "sleep 30 & echo started"})
		assert.StrContains(t, res.Content, "started")
		assert.True(t, took < 3*time.Second, "took %s", took)
	})
	t.Run("colour codes are removed", func(t *testing.T) {
		res, _ := runShell(t, tool, map[string]any{"command": `printf '\033[32mTest Name\033[0m: x\n'; printf '\033[31mFAIL\033[0m\n' >&2`})
		assert.StrContains(t, res.Content, "Test Name: x\n[stderr]\nFAIL")
		assert.NotContains(t, res.Content, "\x1b")
	})
	t.Run("suga's own settings are not passed on", func(t *testing.T) {
		t.Setenv("SUGA_HOME", "/private/suga")
		t.Setenv("SUGA_MODEL", "x/y")
		t.Setenv("SUGA_KEEP_ME", "kept")
		res, _ := runShell(t, tool, map[string]any{"command": `echo "home=[$SUGA_HOME] model=[$SUGA_MODEL] other=[$SUGA_KEEP_ME]"`})
		assert.StrContains(t, res.Content, "home=[] model=[] other=[kept]")
	})
	t.Run("git does not prompt", func(t *testing.T) {
		res, _ := runShell(t, tool, map[string]any{"command": "echo $GIT_TERMINAL_PROMPT $GIT_PAGER"})
		assert.StrContains(t, res.Content, "0 cat")
	})
}
