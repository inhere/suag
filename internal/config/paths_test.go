package axconfig

import (
	"path/filepath"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/testutil/assert"
)

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
