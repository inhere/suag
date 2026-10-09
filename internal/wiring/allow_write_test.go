package wiring

import (
	"context"
	"slices"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
)

// TestToolsAllowWrite covers where tools.allow_write applies: chat only, never
// over --read-only, and never on the ACP path (wiring.Open).
func TestToolsAllowWrite(t *testing.T) {
	resetConfig(t)
	setProviders(t, map[string]any{"openai": map[string]any{"api_key": "k"}})
	assert.NoErr(t, gkconfig.Set("model", "openai/gpt-4o"))
	assert.NoErr(t, gkconfig.Set("tools.allow_write", true))

	hasShell := func(t *testing.T, rt *Runtime) bool {
		t.Helper()
		t.Cleanup(func() { _ = rt.Close() })
		return slices.Contains(rt.Registry.List(), "shell_exec")
	}
	openCLI := func(t *testing.T, opts Options) *Runtime {
		t.Helper()
		rt, err := OpenCLI(context.Background(), opts)
		assert.Require(t, assert.NoErr(t, err))
		return rt
	}

	t.Run("chat with config true exposes write tools", func(t *testing.T) {
		rt := openCLI(t, Options{ChatMode: true})
		assert.True(t, hasShell(t, rt))
		assert.True(t, slices.Contains(rt.Registry.List(), "file_write"))
	})
	t.Run("run ignores the config", func(t *testing.T) {
		assert.False(t, hasShell(t, openCLI(t, Options{})))
	})
	t.Run("run with --allow-write still works", func(t *testing.T) {
		assert.True(t, hasShell(t, openCLI(t, Options{AllowWrite: true})))
	})
	t.Run("read-only wins over the config", func(t *testing.T) {
		rt := openCLI(t, Options{ChatMode: true, ReadOnly: true})
		assert.False(t, hasShell(t, rt))
		assert.False(t, slices.Contains(rt.Registry.List(), "file_write"))
	})
	t.Run("read-only wins over the flag", func(t *testing.T) {
		assert.False(t, hasShell(t, openCLI(t, Options{ChatMode: true, AllowWrite: true, ReadOnly: true})))
	})
	t.Run("ACP Open does not read the config", func(t *testing.T) {
		rt, err := Open(context.Background(), Options{Workspace: t.TempDir(), RuleCWD: t.TempDir()})
		assert.Require(t, assert.NoErr(t, err))
		assert.False(t, hasShell(t, rt))
	})
	t.Run("chat with config false has no write tools", func(t *testing.T) {
		assert.NoErr(t, gkconfig.Set("tools.allow_write", false))
		t.Cleanup(func() { _ = gkconfig.Set("tools.allow_write", true) })
		assert.False(t, hasShell(t, openCLI(t, Options{ChatMode: true})))
	})
	t.Run("non-boolean value is a startup error in chat", func(t *testing.T) {
		for _, value := range []any{"true", 1, "yes"} {
			assert.NoErr(t, gkconfig.Set("tools.allow_write", value))
			_, err := OpenCLI(context.Background(), Options{ChatMode: true})
			assert.Err(t, err)
			assert.StrContains(t, err.Error(), "tools.allow_write must be a boolean")
		}
		assert.NoErr(t, gkconfig.Set("tools.allow_write", true))
	})
}
