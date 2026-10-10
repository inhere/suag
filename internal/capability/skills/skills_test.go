package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestParse(t *testing.T) {
	t.Run("frontmatter and body", func(t *testing.T) {
		s, body, err := Parse([]byte("---\nname: gofer-usage\ndescription: >\n  Use gofer\n  from a container.\nallowed-tools: [Bash, Read]\n---\n\n# Gofer\n\nRun jobs.\n"))
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "gofer-usage", s.Name)
		assert.Eq(t, "Use gofer from a container.", s.Description)
		assert.Eq(t, []string{"Bash", "Read"}, s.AllowedTools)
		assert.Eq(t, "# Gofer\n\nRun jobs.\n", body)
	})
	t.Run("CRLF and BOM", func(t *testing.T) {
		s, body, err := Parse([]byte("\xef\xbb\xbf---\r\nname: x\r\ndescription: d\r\nallowed-tools: Bash, Read\r\n---\r\nbody\r\n"))
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "x", s.Name)
		assert.Eq(t, []string{"Bash", "Read"}, s.AllowedTools)
		assert.Eq(t, "body\n", body)
	})
	t.Run("a --- rule inside the body stays", func(t *testing.T) {
		_, body, err := Parse([]byte("---\nname: x\n---\nA\n\n---\n\nB\n"))
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "A\n\n---\n\nB\n", body)
	})
	t.Run("errors", func(t *testing.T) {
		for _, bad := range []string{"# no frontmatter", "---\nname: x\n", "---\ndescription: no name\n---\n", "---\nname: [\n---\n"} {
			_, _, err := Parse([]byte(bad))
			assert.Err(t, err, bad)
		}
	})
}

func TestLoadRoots(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	write := func(dir, content string) {
		assert.NoErr(t, os.MkdirAll(dir, 0o755))
		assert.NoErr(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644))
	}
	write(filepath.Join(first, "dup"), "---\nname: dup\ndescription: first\n---\n")
	write(filepath.Join(second, "dup"), "---\nname: dup\ndescription: second\n---\n")
	write(filepath.Join(second, "group", "nested"), "---\nname: nested\ndescription: n\n---\n")
	write(filepath.Join(second, "broken"), "no frontmatter")
	write(filepath.Join(second, "a", "b", "too-deep"), "---\nname: deep\n---\n")

	reg := LoadRoots([]string{filepath.Join(first, "missing"), first, second})
	assert.Eq(t, 2, reg.Len())
	dup, ok := reg.Get("DUP")
	assert.True(t, ok)
	assert.Eq(t, "first", dup.Description)
	assert.Eq(t, filepath.Join(first, "dup"), dup.Dir)
	_, ok = reg.Get("nested")
	assert.True(t, ok)
	_, ok = reg.Get("deep")
	assert.False(t, ok)
}
