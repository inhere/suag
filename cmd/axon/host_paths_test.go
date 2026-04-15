package main

import (
	"path/filepath"
	"testing"

	"github.com/gookit/config/v2"
	"github.com/gookit/goutil/testutil/assert"
)

func TestResolveHostAgentcoreRunRoot_DefaultsUnderDB(t *testing.T) {
	config.Reset()
	t.Cleanup(config.Reset)

	root := resolveHostAgentcoreRunRoot()

	assert.Eq(t, filepath.Join("db", "agentcore", "host"), root)
}

func TestResolveHostAgentcoreRunRoot_AppendsHostToConfiguredRoot(t *testing.T) {
	config.Reset()
	t.Cleanup(config.Reset)

	_ = config.Set("agentcore.run_root", filepath.Join("data", "runtime"))

	root := resolveHostAgentcoreRunRoot()

	assert.Eq(t, filepath.Join("data", "runtime", "host"), root)
}
