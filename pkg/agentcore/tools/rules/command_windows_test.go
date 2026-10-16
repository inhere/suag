//go:build windows

package rules

import (
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestRuleMatchesCommandWindowsComparison(t *testing.T) {
	tests := []struct {
		name   string
		rule   Rule
		target Target
		want   bool
	}{
		{
			name:   "case is ignored",
			rule:   Rule{Tool: "shell_exec", Command: []string{"go test *"}, Action: Allow},
			target: Target{Tool: "shell_exec", Shell: ShellPowerShell, Windows: true, Command: "GO TEST ./PKG"}, want: true,
		},
		{
			name:   "executable suffix is ignored",
			rule:   Rule{Tool: "shell_exec", Command: []string{"go test *"}, Action: Allow},
			target: Target{Tool: "shell_exec", Shell: ShellCmd, Windows: true, Command: "go.exe test ./pkg"}, want: true,
		},
		{
			name:   "argument suffix remains significant",
			rule:   Rule{Tool: "shell_exec", Command: []string{"go test ./pkg"}, Action: Allow},
			target: Target{Tool: "shell_exec", Shell: ShellCmd, Windows: true, Command: "go test ./pkg.exe"}, want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Eq(t, tt.want, tt.rule.MatchesCommand(tt.target))
		})
	}
}
