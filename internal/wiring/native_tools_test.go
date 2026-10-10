package wiring

import (
	"context"
	"errors"
	"slices"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/internal/platform/tools/native"
	agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"
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

func TestWebSearchIsARegisteredReadTool(t *testing.T) {
	resetConfig(t)
	setProviders(t, map[string]any{"openai": map[string]any{"api_key": "k"}})
	assert.NoErr(t, gkconfig.Set("model", "openai/gpt-4o"))

	t.Run("read-only with a key", func(t *testing.T) {
		assert.NoErr(t, gkconfig.Set("serpapi_key", "k"))
		rt, err := OpenCLI(context.Background(), Options{})
		assert.Require(t, assert.NoErr(t, err))
		t.Cleanup(func() { _ = rt.Close() })
		assert.Contains(t, rt.Registry.List(), "web_search")
		assert.NotContains(t, rt.Registry.List(), "shell_exec")
	})
	t.Run("no key, no tool", func(t *testing.T) {
		assert.NoErr(t, gkconfig.Set("serpapi_key", ""))
		rt, err := OpenCLI(context.Background(), Options{AllowWrite: true})
		assert.Require(t, assert.NoErr(t, err))
		t.Cleanup(func() { _ = rt.Close() })
		assert.NotContains(t, rt.Registry.List(), "web_search")
	})
}

// TestDisabledToolHints checks the enable hint each entry gives for a write
// tool it did not register, and that web_search without a key is listed too.
func TestDisabledToolHints(t *testing.T) {
	resetConfig(t)
	setProviders(t, map[string]any{"openai": map[string]any{"api_key": "k"}})
	assert.NoErr(t, gkconfig.Set("model", "openai/gpt-4o"))

	call := func(t *testing.T, rt *Runtime, err error, name string) error {
		t.Helper()
		assert.Require(t, assert.NoErr(t, err))
		t.Cleanup(func() { _ = rt.Close() })
		_, execErr := rt.tools.ExecuteTool(context.Background(), agentcoreapi.ToolCall{CallID: "c", Name: name})
		return execErr
	}
	for _, tc := range []struct {
		name string
		open func() (*Runtime, error)
		want string
	}{
		{"run", func() (*Runtime, error) { return OpenCLI(context.Background(), Options{}) }, "加 --allow-write"},
		{"chat", func() (*Runtime, error) { return OpenCLI(context.Background(), Options{ChatMode: true}) }, "tools.allow_write: true"},
		{"read-only", func() (*Runtime, error) { return OpenCLI(context.Background(), Options{ReadOnly: true}) }, "--read-only 启动"},
		{"ACP", func() (*Runtime, error) {
			return Open(context.Background(), Options{Workspace: t.TempDir(), RuleCWD: t.TempDir()})
		}, "read_only 模式"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, err := tc.open()
			execErr := call(t, rt, err, "shell_exec")
			assert.ErrIs(t, execErr, tools.ErrDenied)
			assert.StrContains(t, execErr.Error(), tc.want)
		})
	}
	t.Run("web_search without a key", func(t *testing.T) {
		rt, err := OpenCLI(context.Background(), Options{AllowWrite: true})
		execErr := call(t, rt, err, "web_search")
		assert.ErrIs(t, execErr, tools.ErrDenied)
		assert.StrContains(t, execErr.Error(), "serpapi_key")
	})
	t.Run("unknown name is not a denial", func(t *testing.T) {
		rt, err := OpenCLI(context.Background(), Options{})
		execErr := call(t, rt, err, "bash")
		assert.Err(t, execErr)
		assert.False(t, errors.Is(execErr, tools.ErrDenied))
		assert.StrContains(t, execErr.Error(), "available tools:")
	})
}
