package axconfig

import (
	"path/filepath"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
)

func TestResolveCoreStorePath(t *testing.T) {
	t.Run("default under user home", func(t *testing.T) {
		gkconfig.Reset()
		t.Cleanup(gkconfig.Reset)
		t.Setenv("SUAG_HOME", "")
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)

		got, err := ResolveCoreStorePath()
		assert.NoErr(t, err)
		assert.Eq(t, filepath.Join(home, ".suag", "db", "sessions.db"), got)
	})

	t.Run("SUAG_HOME replaces the root", func(t *testing.T) {
		gkconfig.Reset()
		t.Cleanup(gkconfig.Reset)
		root := t.TempDir()
		t.Setenv("SUAG_HOME", root)

		got, err := ResolveCoreStorePath()
		assert.NoErr(t, err)
		assert.Eq(t, filepath.Join(root, "db", "sessions.db"), got)
	})
}

func TestResolveWorkspacePath(t *testing.T) {
	t.Run("unset is the working directory", func(t *testing.T) {
		gkconfig.Reset()
		t.Cleanup(gkconfig.Reset)
		t.Setenv("SUAG_HOME", t.TempDir())
		dir := t.TempDir()
		t.Chdir(dir)
		got, err := filepath.EvalSymlinks(ResolveWorkspacePath())
		assert.NoErr(t, err)
		want, err := filepath.EvalSymlinks(dir)
		assert.NoErr(t, err)
		assert.Eq(t, want, got)
	})
	t.Run("configured value wins", func(t *testing.T) {
		gkconfig.Reset()
		t.Cleanup(gkconfig.Reset)
		dir := t.TempDir()
		_ = gkconfig.Set("workspace", dir)
		t.Chdir(t.TempDir())
		assert.Eq(t, dir, ResolveWorkspacePath())
	})
}
