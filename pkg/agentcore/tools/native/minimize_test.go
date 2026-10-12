package native

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suag/pkg/agentcore/api"
)

func shellCall(cmd string) api.ToolCall {
	return api.ToolCall{Name: "shell_exec", Arguments: map[string]any{"command": cmd}}
}

func verbosePass(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "=== RUN   TestCase%d\n    case_test.go:%d: some log line %d\n--- PASS: TestCase%d (0.00s)\n    case_test.go:%d: a log line after pass\n", i, 10+i, i, i, 20+i)
	}
	return b.String()
}

func TestMinimizeGoTest(t *testing.T) {
	t.Run("all passed: one line per package and a summary", func(t *testing.T) {
		out := "=== gofmt ===\n" + verbosePass(20) + "PASS\nok  \tgithub.com/x/schedule\t1.2s\n[exit code: 0]"
		tidy, ok := MinimizeToolOutput(shellCall("gofmt -l .; go test -v ./schedule/"), out)
		assert.Require(t, assert.True(t, ok))
		assert.Eq(t, "=== gofmt ===\nok  \tgithub.com/x/schedule\t1.2s\n[exit code: 0]\ngo test: 1 package(s) ok, 20 test(s) passed", tidy)
	})
	t.Run("failures keep their messages, passes are dropped", func(t *testing.T) {
		out := verbosePass(15) + "=== RUN   TestCancel\n    schedule_test.go:161: running task not cancelled\n" +
			"--- FAIL: TestCancel (0.60s)\n    schedule_test.go:161: expected cancel\n" + verbosePass(3) +
			"FAIL\nFAIL\tgithub.com/x/schedule\t2.9s\n[exit code: 1]"
		tidy, ok := MinimizeToolOutput(shellCall("go test ./schedule/"), out)
		assert.Require(t, assert.True(t, ok))
		assert.StrContains(t, tidy, "schedule_test.go:161: running task not cancelled") // log of the failing test
		assert.StrContains(t, tidy, "--- FAIL: TestCancel (0.60s)\n    schedule_test.go:161: expected cancel")
		assert.StrContains(t, tidy, "FAIL\tgithub.com/x/schedule")
		assert.NotContains(t, tidy, "TestCase")
		assert.NotContains(t, tidy, "package(s) ok")
	})
	t.Run("build errors are kept", func(t *testing.T) {
		out := "# github.com/x/schedule\nschedule.go:12:3: undefined: foo\n" + verbosePass(10) + "FAIL\tgithub.com/x/schedule [build failed]\n[exit code: 1]"
		tidy, ok := MinimizeToolOutput(shellCall("go test ./..."), out)
		assert.Require(t, assert.True(t, ok))
		assert.StrContains(t, tidy, "schedule.go:12:3: undefined: foo")
		assert.NotContains(t, tidy, "package(s) ok")
	})
}

func TestMinimizeGeneric(t *testing.T) {
	t.Run("colours, progress and repeats", func(t *testing.T) {
		out := "\x1b[32mstart\x1b[0m\n" + strings.Repeat("downloading 10%\rdownloading 50%\rdownloading 100%\n", 1) +
			strings.Repeat("warning: deprecated\n", 40) + "done\n[exit code: 0]"
		tidy, ok := MinimizeToolOutput(shellCall("npm install"), out)
		assert.Require(t, assert.True(t, ok))
		assert.Eq(t, "start\ndownloading 100%\nwarning: deprecated\n[previous line repeated 39 more times]\ndone\n[exit code: 0]", tidy)
	})
	t.Run("left alone", func(t *testing.T) {
		_, ok := MinimizeToolOutput(shellCall("go test ./..."), "ok  \tx\t0.1s\n[exit code: 0]") // small
		assert.False(t, ok)
		_, ok = MinimizeToolOutput(api.ToolCall{Name: "file_read"}, strings.Repeat("=== RUN x\n", 200)) // not shell
		assert.False(t, ok)
		plain := strings.Repeat("line of ordinary output that differs ", 1)
		var b strings.Builder
		for i := 0; i < 100; i++ {
			fmt.Fprintf(&b, "%s%d\n", plain, i)
		}
		_, ok = MinimizeToolOutput(shellCall("ls -l"), b.String()) // nothing to tidy
		assert.False(t, ok)
	})
	t.Run("long outputs keep head and tail", func(t *testing.T) {
		var b strings.Builder
		for i := 0; i < 1000; i++ {
			fmt.Fprintf(&b, "row %d\n", i)
		}
		tidy, ok := MinimizeToolOutput(shellCall("seq"), b.String())
		assert.Require(t, assert.True(t, ok))
		assert.StrContains(t, tidy, "row 0\n")
		assert.StrContains(t, tidy, "row 999")
		assert.StrContains(t, tidy, "lines omitted")
	})
}

func TestMinimizeSkipsPipedCommands(t *testing.T) {
	out := verbosePass(20) + "PASS\nok  \tgithub.com/x/schedule\t1.2s\n[exit code: 0]"
	t.Run("a piped command keeps the lines it filtered for", func(t *testing.T) {
		for _, cmd := range []string{
			`go test -v ./x/... 2>&1 | Select-String -Pattern "^(=== RUN|--- |ok|FAIL|PASS)"`,
			"go test -v ./... | grep -E 'PASS|FAIL'",
			"go test -v ./... |tail -50",
		} {
			tidy, ok := MinimizeToolOutput(shellCall(cmd), out)
			if ok { // only the generic cleanup may apply; the test lines stay
				assert.StrContains(t, tidy, "--- PASS: TestCase0")
			}
		}
	})
	t.Run("a | inside quotes or || is no pipe", func(t *testing.T) {
		for _, cmd := range []string{
			"go test -v -run 'TestA|TestB' ./...",
			`go test -v -run "TestA|TestB" ./...`,
			"go test -v ./... || echo failed",
			`cd "D:\work\x\" ; go test -v ./...`,
		} {
			tidy, ok := MinimizeToolOutput(shellCall(cmd), out)
			assert.Require(t, assert.True(t, ok))
			assert.NotContains(t, tidy, "--- PASS: TestCase0")
		}
	})
}
