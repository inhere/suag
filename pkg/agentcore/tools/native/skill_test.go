package native

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suag/pkg/agentcore/skills"
)

func TestSkillTool(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	assert.NoErr(t, os.MkdirAll(filepath.Join(dir, "references"), 0o755))
	assert.NoErr(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: demo\ndescription: d\n---\n# Demo\nDo it.\n"), 0o644))
	assert.NoErr(t, os.WriteFile(filepath.Join(dir, "references", "usage.md"), []byte("usage text"), 0o644))
	assert.NoErr(t, os.WriteFile(filepath.Join(root, "secret.txt"), []byte("secret"), 0o644))
	tool := NewSkillTool(skills.LoadRoots([]skills.Root{{Dir: root}}), 0)

	t.Run("body and file list", func(t *testing.T) {
		out := execTool(t, tool, map[string]any{"name": "demo"})
		assert.StrContains(t, out, "# Skill: demo\nDirectory: "+dir)
		assert.StrContains(t, out, "Files (read with file=...): references/usage.md")
		assert.StrContains(t, out, "# Demo\nDo it.")
	})
	t.Run("a file of the skill", func(t *testing.T) {
		assert.Eq(t, "usage text", execTool(t, tool, map[string]any{"name": "demo", "file": "references/usage.md"}))
	})
	t.Run("files outside the skill directory are refused", func(t *testing.T) {
		_, err := tool.Execute(t.Context(), callWith(map[string]any{"name": "demo", "file": "../secret.txt"}))
		assert.Err(t, err)
		if err := os.Symlink(filepath.Join(root, "secret.txt"), filepath.Join(dir, "link.txt")); err == nil {
			_, err := tool.Execute(t.Context(), callWith(map[string]any{"name": "demo", "file": "link.txt"}))
			assert.Err(t, err)
		}
	})
	t.Run("unknown skill lists the available ones", func(t *testing.T) {
		_, err := tool.Execute(t.Context(), callWith(map[string]any{"name": "nope"}))
		assert.ErrMsgContains(t, err, `no skill matches "nope"`)
	})
	t.Run("query * lists every name by source", func(t *testing.T) {
		ws := t.TempDir()
		wsDir := filepath.Join(ws, "proj")
		assert.NoErr(t, os.MkdirAll(wsDir, 0o755))
		assert.NoErr(t, os.WriteFile(filepath.Join(wsDir, "SKILL.md"), []byte("---\nname: proj\n---\n"), 0o644))
		both := NewSkillTool(skills.LoadRoots([]skills.Root{{Dir: ws, Workspace: true}, {Dir: root}}), 0)
		assert.Eq(t, "2 skills (load one with name; search descriptions with keywords):\nworkspace: proj\nuser: demo",
			execTool(t, both, map[string]any{"query": "*"}))
	})
	t.Run("query searches", func(t *testing.T) {
		assert.StrContains(t, execTool(t, tool, map[string]any{"query": "demo"}), "- demo — d")
		_, err := tool.Execute(t.Context(), callWith(map[string]any{}))
		assert.ErrMsgContains(t, err, "pass name to load a skill or query to search")
	})
}
