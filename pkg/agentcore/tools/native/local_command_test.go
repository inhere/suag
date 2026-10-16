package native

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/pkg/agentcore/api"
)

// TestLocalCommand covers what chat's "!cmd" lines run on: the shell, the
// workspace as working directory, the exit code, the output kept for the model
// and the two ways a run ends early (the person's Ctrl-C and the timeout).
func TestLocalCommand(t *testing.T) {
	ws := t.TempDir()
	run := NewLocalCommand(LocalCommandOptions{Workspace: ws})

	t.Run("runs a command and keeps its output for the model", func(t *testing.T) {
		var stream bytes.Buffer
		res, err := run.Run(context.Background(), "echo hi", &stream)
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, 0, res.ExitCode)
		assert.Eq(t, "hi", strings.TrimSpace(stream.String()), "the output is streamed as it comes")
		assert.StrContains(t, res.ModelText, "hi")
		assert.StrContains(t, res.ModelText, "退出码 0")
		assert.StrContains(t, res.ModelText, "[exit code: 0]")
		assert.True(t, res.Bytes >= 3, "the written bytes are counted")
		assert.True(t, !res.TimedOut && !res.Cancelled)
	})

	t.Run("runs in the workspace", func(t *testing.T) {
		_, err := run.Run(context.Background(), "echo probe > probe.txt", nil)
		assert.NoErr(t, err)
		_, statErr := os.Stat(filepath.Join(ws, "probe.txt"))
		assert.NoErr(t, statErr)
	})

	t.Run("a non-zero exit code is a result, not an error", func(t *testing.T) {
		res, err := run.Run(context.Background(), "exit 3", nil)
		assert.NoErr(t, err)
		assert.Eq(t, 3, res.ExitCode)
		assert.StrContains(t, res.ModelText, "退出码 3")
		assert.StrContains(t, res.ModelText, "[exit code: 3]")
	})

	t.Run("a cancelled context kills the command", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(300 * time.Millisecond)
			cancel()
		}()
		res, err := run.Run(ctx, "sleep 30", nil)
		assert.NoErr(t, err)
		assert.True(t, res.Cancelled, "the run reports the cancellation")
		assert.True(t, !res.TimedOut)
		assert.True(t, res.Duration < 20*time.Second, "the command was killed, not waited for")
		assert.StrContains(t, res.ModelText, "已终止")
	})

	t.Run("the timeout kills the command", func(t *testing.T) {
		quick := NewLocalCommand(LocalCommandOptions{Workspace: ws, Timeout: 300 * time.Millisecond})
		res, err := quick.Run(context.Background(), "sleep 30", nil)
		assert.NoErr(t, err)
		assert.True(t, res.TimedOut)
		assert.True(t, !res.Cancelled, "a timeout is not a cancellation")
		assert.StrContains(t, res.ModelText, "超时已终止")
	})

	t.Run("the minimizer tidies the model text", func(t *testing.T) {
		var asked []string
		tidy := NewLocalCommand(LocalCommandOptions{
			Workspace: ws,
			Minimize: func(call api.ToolCall, _ string) (string, bool) {
				asked = append(asked, call.Name+" "+call.Arguments["command"].(string))
				return "TIDY", true
			},
		})
		res, err := tidy.Run(context.Background(), "echo hi", nil)
		assert.NoErr(t, err)
		assert.Eq(t, []string{"shell_exec echo hi"}, asked, "the rules see the call as a shell_exec one")
		assert.StrContains(t, res.ModelText, "TIDY")
		assert.StrNotContains(t, res.ModelText, "[exit code: 0]", "the tidy text replaces the raw output")
	})

	t.Run("a shell that cannot start is an error", func(t *testing.T) {
		broken := NewLocalCommand(LocalCommandOptions{Workspace: ws, Shell: "suga-no-such-shell"})
		_, err := broken.Run(context.Background(), "echo hi", nil)
		assert.Err(t, err)
	})
}

// TestCommandOutputBoundsMemory: the copy kept for the model is bounded - a
// command that never stops printing cannot grow the chat's memory, while the
// person still sees every byte.
func TestCommandOutputBoundsMemory(t *testing.T) {
	var stream bytes.Buffer
	out := &commandOutput{stream: &stream, budget: 100}
	chunk := []byte(strings.Repeat("x", 40) + "\n")
	for i := 0; i < 50; i++ {
		n, err := out.Write(chunk)
		assert.NoErr(t, err)
		assert.Eq(t, len(chunk), n)
	}
	assert.Eq(t, 50*len(chunk), out.bytes, "every byte is counted")
	assert.Eq(t, 50*len(chunk), stream.Len(), "every byte reaches the terminal")
	text := out.text()
	assert.True(t, len(text) <= 100+120, "the kept text stays around the budget: %d", len(text))
	assert.StrContains(t, text, "省略")
	assert.Eq(t, 50*len(chunk)-100, out.dropped, "the marker names what was dropped")
}
