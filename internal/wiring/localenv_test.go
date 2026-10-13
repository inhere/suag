package wiring

import (
	"os"
	"path/filepath"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
)

func TestLoadLocalEnv(t *testing.T) {
	workspace := manifestConfig(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SUAG_HOME", t.TempDir())
	t.Setenv("SUAG_HOOKS", "")
	assert.NoErr(t, os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("RULE-A"), 0o644))
	assert.NoErr(t, os.WriteFile(filepath.Join(workspace, "CLAUDE.md"), []byte("RULE-C"), 0o644))
	writeSkill(t, filepath.Join(workspace, "skills", "a"), "alpha", "Workspace skill.")
	writeSkill(t, filepath.Join(home, ".agents", "skills", "b"), "beta", "User skill.")
	assert.NoErr(t, gkconfig.Set("skills.list_max", 1))
	assert.NoErr(t, gkconfig.Set("hooks.command", []string{"hook-cmd"}))

	env, err := LoadLocalEnv(workspace)
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, workspace, env.Workspace)
	t.Run("tools with their schemas", func(t *testing.T) {
		var found bool
		for _, tool := range env.Tools {
			if tool.Name == "file_read" {
				found = true
				assert.NotEmpty(t, tool.Description)
				assert.NotNil(t, tool.InputSchema["properties"])
			}
		}
		assert.True(t, found)
		assert.Eq(t, len(env.Manifest.Tools), len(env.Tools))
	})
	t.Run("skills and how they are shown", func(t *testing.T) {
		shown := map[string]string{}
		for _, s := range env.Skills {
			shown[s.Name] = s.Shown
		}
		assert.Eq(t, map[string]string{"alpha": "listed", "beta": "named"}, shown)
		assert.Eq(t, 2, env.SkillsStats.Total)
		assert.StrContains(t, env.SkillsText, "): beta.")
	})
	t.Run("rules: only AGENTS.md", func(t *testing.T) {
		assert.Eq(t, "AGENTS.md", env.Rules.File)
		assert.StrContains(t, env.Rules.Text, "RULE-A")
		assert.NotContains(t, env.Rules.Text, "RULE-C")
		assert.NotEmpty(t, env.Rules.Hash)
		var existing int
		for _, f := range env.Rules.Files {
			if f.Exists {
				existing++
			}
		}
		assert.Eq(t, 1, existing)
	})
	t.Run("hooks and MCP servers", func(t *testing.T) {
		assert.True(t, env.Hooks.Enabled)
		assert.StrContains(t, env.Hooks.Config, "hook-cmd")
		assert.Require(t, assert.Len(t, env.MCPServers, 1))
		assert.Eq(t, "srv", env.MCPServers[0].ID)
	})
}
