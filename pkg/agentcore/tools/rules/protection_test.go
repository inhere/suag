package rules

import (
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestProtectedPathAndClampAllow(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "repo")
	home := filepath.Join(t.TempDir(), ".suga")
	userRules := filepath.Join(home, "approval-rules.yaml")
	projectRules := filepath.Join(workspace, ".suga", "approval-rules.yaml")
	for _, tc := range []struct {
		name      string
		path      string
		mode      Mode
		protected bool
		clamped   Action
	}{
		{name: "workspace git file", path: ".git/config", mode: ModeRun, protected: true, clamped: Deny},
		{name: "workspace suga file", path: ".suga/output-rules.yaml", mode: ModeChat, protected: true, clamped: Ask},
		{name: "user rule file", path: userRules, mode: ModeACP, protected: true, clamped: Ask},
		{name: "project rule file", path: projectRules, mode: ModeRun, protected: true, clamped: Deny},
		{name: "ordinary file", path: "docs/note.md", mode: ModeRun, protected: false, clamped: Allow},
		{name: "prefix is not a root", path: filepath.Join(workspace+"-copy", ".git", "config"), mode: ModeRun, protected: false, clamped: Allow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := Target{Tool: "file_write", Root: workspace, Path: tc.path, Mode: tc.mode}
			assert.Eq(t, tc.protected, IsProtectedPath(target, workspace, home, userRules, projectRules))
			assert.Eq(t, tc.clamped, ClampAllow(Allow, target, workspace, home, userRules, projectRules))
		})
	}
}

func TestProtectedPathDoesNotApplyToReadToolsOrMCP(t *testing.T) {
	workspace := t.TempDir()
	target := Target{Tool: "file_read", Root: workspace, Path: ".git/config"}
	assert.False(t, IsProtectedPath(target, workspace, t.TempDir()))
	target = Target{Tool: "file_write", MCP: true, Root: workspace, Path: ".git/config"}
	assert.False(t, IsProtectedPath(target, workspace, t.TempDir()))
}
