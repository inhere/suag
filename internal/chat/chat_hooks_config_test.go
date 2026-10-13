package chat

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
	sugahooks "github.com/inhere/suga/internal/hooks"
	"github.com/inhere/suga/internal/personal"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore/mock"
	"github.com/inhere/suga/pkg/agentcore/tools"
)

// TestFakeHookProcess is the fake hook command for chat tests: the test binary
// re-runs itself with -test.run=^TestFakeHookProcess$ and appends the hook args
// and event name to SUGA_FAKE_HOOK_LOG. It answers with an empty response.
func TestFakeHookProcess(t *testing.T) {
	logPath := os.Getenv("SUGA_FAKE_HOOK_LOG")
	if logPath == "" {
		return
	}
	var event map[string]any
	_ = json.NewDecoder(bufio.NewReader(os.Stdin)).Decode(&event)
	line, _ := json.Marshal(map[string]any{"args": os.Args[2:], "event": event["hook_event_name"], "tool": event["tool_name"]})
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		os.Exit(3)
	}
	_, _ = file.Write(append(line, '\n'))
	_ = file.Close()
	// Exit before the testing framework prints PASS on stdout.
	os.Exit(0)
}

type recordedHookCall struct {
	Args  []string `json:"args"`
	Event string   `json:"event"`
	Tool  string   `json:"tool"`
}

// configureFakeHook points hooks.command at TestFakeHookProcess and returns the log path.
func configureFakeHook(t *testing.T) string {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "hook.log")
	t.Setenv("SUGA_HOOKS", "")
	t.Setenv("SUGA_FAKE_HOOK_LOG", logPath)
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	assert.NoErr(t, gkconfig.Set("hooks.command", []string{os.Args[0], "-test.run=^TestFakeHookProcess$", "base"}))
	return logPath
}

func readHookLog(t *testing.T, logPath string) []recordedHookCall {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return nil
	}
	assert.Require(t, assert.NoErr(t, err))
	var calls []recordedHookCall
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var call recordedHookCall
		assert.Require(t, assert.NoErr(t, json.Unmarshal([]byte(line), &call)))
		calls = append(calls, call)
	}
	return calls
}

// newShellToolRunner returns a runner whose model calls shell_exec once, then answers.
func newShellToolRunner(t *testing.T) *personal.Runner {
	t.Helper()
	repo, err := store.Open(newTestStorePath(t))
	assert.Require(t, assert.NoErr(t, err))
	t.Cleanup(func() { _ = repo.Close() })
	registry := tools.NewRegistry()
	registry.Register(appHookOutputTool{})
	runner, err := personal.New(personal.Config{
		Repository: repo,
		Provider: mock.NewProvider(
			mock.ToolCall("call-1", "shell_exec", map[string]any{"command": "print environment"}),
			mock.Final("finished"),
		),
		Tools: registry,
	})
	assert.Require(t, assert.NoErr(t, err))
	return runner
}

func TestPersonalChatRunsConfiguredHookCommand(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	t.Setenv("SUGA_HOME", t.TempDir())

	t.Run("events and stop_args", func(t *testing.T) {
		logPath := configureFakeHook(t)
		assert.NoErr(t, gkconfig.Set("hooks.stop_args", []string{"--wait", "5"}))
		assert.NoErr(t, gkconfig.Set("hooks.post_tool_use.tools", []string{"shell_*"}))
		assert.NoErr(t, gkconfig.Set("hooks.post_tool_use.name", "shell"))
		hooks, err := sugahooks.New()
		assert.Require(t, assert.NoErr(t, err))
		// :quit only after the Stop command ran: an earlier :quit interrupts and may
		// kill the Stop process before it starts, which is the normal interrupt path.
		stdin, input := io.Pipe()
		go func() {
			_, _ = io.WriteString(input, "run command\n")
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				// Raw check: this goroutine must not call t.FailNow via readHookLog.
				if data, _ := os.ReadFile(logPath); strings.Contains(string(data), `"event":"Stop"`) {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			_, _ = io.WriteString(input, ":quit\n")
			_ = input.Close()
		}()
		var stdout, stderr strings.Builder
		assert.NoErr(t, Run(context.Background(), newShellToolRunner(t), Options{}, stdin, &stdout, &stderr, hooks))

		var names []string
		for _, call := range readHookLog(t, logPath) {
			// Interrupt shows up only when :quit wins the race with the Stop answer.
			if call.Event == "Interrupt" {
				assert.Eq(t, []string{"base"}, call.Args)
				continue
			}
			names = append(names, call.Event)
			if call.Event == "Stop" {
				assert.Eq(t, []string{"base", "--wait", "5"}, call.Args)
			} else {
				assert.Eq(t, []string{"base"}, call.Args)
			}
			if call.Event == "PostToolUse" {
				assert.Eq(t, "shell", call.Tool)
			}
		}
		assert.Eq(t, []string{"SessionStart", "UserPromptSubmit", "PostToolUse", "Stop", "SessionEnd"}, names)
	})
	t.Run("no hooks.command runs nothing", func(t *testing.T) {
		gkconfig.Reset()
		logPath := configureFakeHook(t)
		assert.NoErr(t, gkconfig.Set("hooks.command", []string{}))
		assert.NoErr(t, gkconfig.Set("hooks.post_tool_use.tools", []string{"*"}))
		hooks, err := sugahooks.New()
		assert.NoErr(t, err)
		assert.Require(t, assert.Nil(t, hooks))
		var stdout, stderr strings.Builder
		assert.NoErr(t, Run(context.Background(), newShellToolRunner(t), Options{}, strings.NewReader("run command\n:quit\n"), &stdout, &stderr, nil))
		assert.StrContains(t, stdout.String(), "finished")
		assert.Nil(t, readHookLog(t, logPath))
	})
	t.Run("SUGA_HOOKS=off runs nothing", func(t *testing.T) {
		gkconfig.Reset()
		configureFakeHook(t)
		t.Setenv("SUGA_HOOKS", "off")
		hooks, err := sugahooks.New()
		assert.NoErr(t, err)
		assert.Nil(t, hooks)
	})
}

func TestPersonalChatPostToolUseFollowsConfig(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	t.Setenv("SUGA_HOME", t.TempDir())
	for _, tc := range []struct {
		name   string
		filter sugahooks.PostToolFilter
		// want is the PostToolUse tool name; empty means no PostToolUse is sent.
		want string
	}{
		{name: "unconfigured sends none", filter: sugahooks.PostToolFilter{Name: "shell"}},
		{name: "unmatched tool sends none", filter: sugahooks.PostToolFilter{Tools: []string{"file_*", "exec"}}},
		{name: "glob match keeps the name", filter: sugahooks.PostToolFilter{Tools: []string{"shell_*"}}, want: "shell_exec"},
		{name: "exact match is renamed", filter: sugahooks.PostToolFilter{Tools: []string{"shell_exec"}, Name: "shell"}, want: "shell"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hooks := newFakeHookClient()
			hooks.blockFirst = false
			hooks.postTool = tc.filter
			var stdout, stderr strings.Builder
			assert.NoErr(t, Run(context.Background(), newShellToolRunner(t), Options{}, strings.NewReader("run command\n:quit\n"), &stdout, &stderr, hooks))
			var got []string
			for _, event := range hooks.snapshot() {
				if event.Name == "PostToolUse" {
					got = append(got, event.ToolName)
				}
			}
			if tc.want == "" {
				assert.Empty(t, got)
			} else {
				assert.Eq(t, []string{tc.want}, got)
			}
			assert.Eq(t, 1, countHookEvents(hooks.snapshot(), "Stop"))
		})
	}
}
