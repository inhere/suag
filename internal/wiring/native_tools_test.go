package wiring

import (
	"context"
	"slices"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suag/internal/platform/tools/native"
)

// catalogNames returns the native catalog tool names, read tools only unless
// write is set.
func catalogNames(write bool) []string {
	var names []string
	for _, spec := range native.Catalog() {
		if spec.Write && !write {
			continue
		}
		if spec.Name == "web_search" { // needs serpapi_key, unset here
			continue
		}
		names = append(names, spec.Name)
	}
	return names
}

// TestRegisteredToolsFollowTheCatalog pins the run/chat/ACP tool sets to the
// one native catalog plus update_plan, which Open registers in every mode.
func TestRegisteredToolsFollowTheCatalog(t *testing.T) {
	resetConfig(t)
	setProviders(t, map[string]any{"openai": map[string]any{"api_key": "k"}})
	assert.NoErr(t, gkconfig.Set("model", "openai/gpt-4o"))

	names := func(t *testing.T, rt *Runtime, err error) []string {
		t.Helper()
		assert.Require(t, assert.NoErr(t, err))
		t.Cleanup(func() { _ = rt.Close() })
		got := rt.Registry.List()
		slices.Sort(got)
		return got
	}
	want := func(write bool) []string {
		w := append(catalogNames(write), "update_plan")
		slices.Sort(w)
		return w
	}

	t.Run("run read-only", func(t *testing.T) {
		rt, err := OpenCLI(context.Background(), Options{})
		assert.Eq(t, want(false), names(t, rt, err))
	})
	t.Run("run --allow-write", func(t *testing.T) {
		rt, err := OpenCLI(context.Background(), Options{AllowWrite: true})
		assert.Eq(t, want(true), names(t, rt, err))
	})
	t.Run("ACP read-only", func(t *testing.T) {
		rt, err := Open(context.Background(), Options{Workspace: t.TempDir(), RuleCWD: t.TempDir()})
		assert.Eq(t, want(false), names(t, rt, err))
	})
	t.Run("ACP writable", func(t *testing.T) {
		rt, err := Open(context.Background(), Options{Workspace: t.TempDir(), RuleCWD: t.TempDir(), AllowWrite: true})
		assert.Eq(t, want(true), names(t, rt, err))
	})
}
