package native

// Acceptance test injected after the run (the agent does not see it).

import (
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestAcceptMinimizeSkipsPipedCommands(t *testing.T) {
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
