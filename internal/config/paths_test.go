package axconfig

import (
	"path/filepath"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/testutil/assert"
)

func TestPersonalPathsUseUserDataDirectoryWithoutChangingHostDefaults(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	t.Setenv("DATABASE_URL", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	dbPath, err := ResolvePersonalSessionDBPath()
	assert.NoErr(t, err)
	runRoot, err := ResolvePersonalAgentcoreRunRoot()
	assert.NoErr(t, err)
	assert.Eq(t, filepath.Join(home, ".suag", "db", "suag.db"), dbPath)
	assert.Eq(t, filepath.Join(home, ".suag", "db", "agentcore", "host"), runRoot)
	assert.Eq(t, filepath.Join("db", "suag.db"), ResolveSessionDBPath())
	assert.Eq(t, filepath.Join("db", "agentcore", "host"), ResolveHostAgentcoreRunRoot())
}

func TestPersonalPathsHonorExplicitOverrides(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	t.Setenv("DATABASE_URL", "")
	dbPath := filepath.Join(t.TempDir(), "old-sessions.db")
	runRoot := filepath.Join(t.TempDir(), "old-runs")
	_ = gkconfig.Set("database_url", "sqlite://"+filepath.ToSlash(dbPath)+"?mode=rwc")
	_ = gkconfig.Set("agentcore.run_root", runRoot)

	gotDB, err := ResolvePersonalSessionDBPath()
	assert.NoErr(t, err)
	gotRuns, err := ResolvePersonalAgentcoreRunRoot()
	assert.NoErr(t, err)
	assert.Eq(t, dbPath, gotDB)
	assert.Eq(t, filepath.Join(runRoot, "host"), gotRuns)
}

func TestPersonalPathsUseSuagHome(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	t.Setenv("DATABASE_URL", "")
	root := t.TempDir()
	t.Setenv("SUAG_HOME", root)
	dbPath, err := ResolvePersonalSessionDBPath()
	assert.NoErr(t, err)
	assert.Eq(t, filepath.Join(root, "db", "suag.db"), dbPath)
	runRoot, err := ResolvePersonalAgentcoreRunRoot()
	assert.NoErr(t, err)
	assert.Eq(t, filepath.Join(root, "db", "agentcore", "host"), runRoot)
}

func TestResolveHostAgentcoreRunRoot_DefaultsUnderDB(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	root := ResolveHostAgentcoreRunRoot()

	assert.Eq(t, filepath.Join("db", "agentcore", "host"), root)
}

func TestResolveHostAgentcoreRunRoot_AppendsHostToConfiguredRoot(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	_ = gkconfig.Set("agentcore.run_root", filepath.Join("data", "runtime"))

	root := ResolveHostAgentcoreRunRoot()

	assert.Eq(t, filepath.Join("data", "runtime", "host"), root)
}

func TestResolveHostMemoryDraftRoot_DefaultsUnderHostRuntimeRoot(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	root := ResolveHostMemoryDraftRoot()

	assert.Eq(t, filepath.Join("db", "agentcore", "host"), root)
}

func TestResolveHostMemoryDraftRoot_UsesConfiguredRoot(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	_ = gkconfig.Set("host.memory.root", filepath.Join("data", "memory"))

	root := ResolveHostMemoryDraftRoot()

	assert.Eq(t, filepath.Join("data", "memory"), root)
}
