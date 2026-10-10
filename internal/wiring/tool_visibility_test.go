package wiring

import (
	"context"
	"errors"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
	agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"
	"github.com/inhere/suag/pkg/agentcore/tools"
)

func TestNewToolVisibility(t *testing.T) {
	build := func(t *testing.T, mode string) (*tools.Registry, *deniedCounter, *tools.Visibility, error) {
		t.Helper()
		gkconfig.Reset()
		t.Cleanup(gkconfig.Reset)
		if mode != "" {
			assert.NoErr(t, gkconfig.Set("tools.deferred", mode))
		}
		reg := tools.NewRegistry()
		reg.Register(newCapabilityStubTool("other_tool"))
		reg.Register(newServerStubTool("srv", "srv_write"))
		cliGate, err := newCLIMCPGate(false)
		assert.NoErr(t, err)
		gate := &deniedCounter{inner: cliGate}
		vis, err := newToolVisibility(reg, tools.NewApproval(reg, gate), 128000)
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
		// srv_write is not allowlisted: reaching the approver would record a denial.
		_, execErr := vis.ExecuteTool(context.Background(), agentcoreapi.ToolCall{Name: "srv_write"})
		assert.Err(t, execErr)
		assert.Eq(t, false, errors.Is(execErr, tools.ErrDenied))
		assert.Eq(t, 0, gate.denied)
		// Once loaded, the call reaches the gate and is denied there.
		_, execErr = vis.ExecuteTool(context.Background(), agentcoreapi.ToolCall{Name: "srv_write"})
		assert.Eq(t, true, errors.Is(execErr, tools.ErrDenied))
		assert.Eq(t, 1, gate.denied)
	})
	t.Run("invalid mode is rejected", func(t *testing.T) {
		_, _, _, err := build(t, "sometimes")
		assert.Err(t, err)
	})
}

// deniedCounter wraps an approver and counts the calls it refused.
type deniedCounter struct {
	inner  tools.Approver
	denied int
}

func (d *deniedCounter) Decide(ctx context.Context, call agentcoreapi.ToolCall, tool agentcoreapi.Tool) error {
	err := d.inner.Decide(ctx, call, tool)
	if errors.Is(err, tools.ErrDenied) {
		d.denied++
	}
	return err
}
