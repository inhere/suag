package native

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestIgnoreMatcher(t *testing.T) {
	ws := t.TempDir()
	write := func(rel, text string) {
		full := filepath.Join(ws, filepath.FromSlash(rel))
		assert.NoErr(t, os.MkdirAll(filepath.Dir(full), 0o755))
		assert.NoErr(t, os.WriteFile(full, []byte(text), 0o644))
	}
	write(".gitignore", "# comment\n*.log\n!keep.log\n/tmp/\nbuild/\ndocs/**/draft.md\n\\#hash.txt\nout\n")
	write("sub/.gitignore", "*.gen.go\n!special.log\n/local.txt\n")
	root, err := os.OpenRoot(ws)
	assert.Require(t, assert.NoErr(t, err))
	defer root.Close()

	for _, tc := range []struct {
		rel   string
		isDir bool
		want  bool
	}{
		{"app.log", false, true},
		{"deep/x/app.log", false, true}, // a pattern without "/" matches at any depth
		{"keep.log", false, false},      // "!" re-includes
		{"tmp", true, true},             // "/tmp/" is anchored to the root
		{"sub/tmp", true, false},
		{"tmp", false, false}, // "tmp/" matches directories only
		{"build", true, true},
		{"a/build", true, true},
		{"docs/x/y/draft.md", false, true}, // "**" spans directories
		{"draft.md", false, false},
		{"#hash.txt", false, true}, // "\#" is a literal "#"
		{"out", false, true},
		{"out", true, true},
		{"sub/a.gen.go", false, true},     // nested .gitignore
		{"a.gen.go", false, false},        // ...applies only below its directory
		{"sub/special.log", false, false}, // a deeper rule wins
		{"sub/local.txt", false, true},    // anchored to sub/
		{"sub/x/local.txt", false, false}, // ...not deeper
		{"main.go", false, false},
	} {
		m := newIgnoreMatcher(root, false)
		assert.Eq(t, tc.want, m.ignored(tc.rel, tc.isDir), tc.rel)
	}

	t.Run("no_ignore keeps only .git out", func(t *testing.T) {
		m := newIgnoreMatcher(root, true)
		assert.False(t, m.skipDir("node_modules", "node_modules"))
		assert.False(t, m.skipDir("tmp", "tmp"))
		assert.False(t, m.skipFile("app.log"))
		assert.True(t, m.skipDir(".git", ".git"))
		assert.Eq(t, "", m.note()) // .git is never worth a note
	})
	t.Run("the note names the first skipped dirs", func(t *testing.T) {
		m := newIgnoreMatcher(root, false)
		for _, d := range []string{"tmp", "build", "node_modules", "vendor", "dist", "a/build"} {
			assert.True(t, m.skipDir(d, filepath.Base(d)), d)
		}
		assert.True(t, m.skipFile("x.log"))
		assert.Eq(t, "[skipped 6 ignored dir(s): tmp/, build/, node_modules/, vendor/, dist/ and 1 more; 1 ignored file(s) "+
			"(.gitignore and node_modules/vendor/dist); pass no_ignore: true to include them]", m.note())
	})
}
