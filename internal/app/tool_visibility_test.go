package app

import (
	"context"
	"errors"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/platform/tools"
	agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"
)

func TestNewToolVisibility(t *testing.T) {
	build := func(t *testing.T, mode string) (*tools.Registry, *cliMCPGate, *tools.Visibility, error) {
		t.Helper()
		gkconfig.Reset()
		t.Cleanup(gkconfig.Reset)
		if mode != "" {
			assert.NoErr(t, gkconfig.Set("tools.deferred", mode))
		}
		reg := tools.NewRegistry()
		reg.Register(newCapabilityStubTool("other_tool"))
		reg.Register(newServerStubTool("srv", "srv_write"))
		gate, err := newCLIMCPGate(reg, false)
		assert.NoErr(t, err)
		vis, err := newToolVisibility(reg, gate, 128000)
		return reg, gate, vis, err
	}
	t.Run("auto with small schemas sends everything directly", func(t *testing.T) {
		reg, _, vis, err := build(t, "")
		assert.NoErr(t, err)
		assert.Eq(t, false, vis.Active())
		_, getErr := reg.Get("tool_search")
		assert.Err(t, getErr)
	})
	t.Run("always defers, registers tool_search, and skips approval for unloaded calls", func(t *testing.T) {
		reg, gate, vis, err := build(t, "always")
		assert.NoErr(t, err)
		assert.Eq(t, true, vis.Active())
		_, getErr := reg.Get("tool_search")
		assert.NoErr(t, getErr)
		listed, _ := vis.ListTools(context.Background())
		var names []string
		for _, s := range listed {
			names = append(names, s.Name)
		}
		assert.Eq(t, []string{"tool_search"}, names)
		// srv_write is not allowlisted: an approved path would mark the gate denied.
		_, execErr := vis.ExecuteTool(context.Background(), agentcoreapi.ToolCall{Name: "srv_write"})
		assert.Err(t, execErr)
		assert.Eq(t, false, errors.Is(execErr, tools.ErrDenied))
		assert.Eq(t, false, gate.WasDenied())
		// Once loaded, the call reaches the gate and is denied there.
		_, execErr = vis.ExecuteTool(context.Background(), agentcoreapi.ToolCall{Name: "srv_write"})
		assert.Eq(t, true, errors.Is(execErr, tools.ErrDenied))
		assert.Eq(t, true, gate.WasDenied())
	})
	t.Run("invalid mode is rejected", func(t *testing.T) {
		_, _, _, err := build(t, "sometimes")
		assert.Err(t, err)
	})
}
