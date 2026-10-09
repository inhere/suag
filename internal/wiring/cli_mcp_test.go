package wiring

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/pkg/agentcore"
)

// newGatedRegistry puts the CLI approver in front of registry, as the runtime does.
func newGatedRegistry(registry *tools.Registry, readOnly bool) (*tools.Approval, error) {
	gate, err := newCLIMCPGate(readOnly)
	if err != nil {
		return nil, err
	}
	return tools.NewApproval(registry, gate), nil
}

func TestCLIMCPGateDefaultsAndOverrides(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	registry := tools.NewRegistry()
	for _, name := range []string{"gofer_issue_list", "gofer_work_get", "gofer_work_report", "gofer_issue_close", "mcp_write"} {
		registry.Register(newServerStubTool("gofer", name))
	}
	gate, err := newGatedRegistry(registry, false)
	assert.NoErr(t, err)
	for _, name := range []string{"gofer_issue_list", "gofer_work_get", "gofer_work_report"} {
		_, err := gate.ExecuteTool(context.Background(), agentcore.ToolCall{Name: name})
		assert.NoErr(t, err)
	}
	for _, name := range []string{"gofer_issue_close", "mcp_write"} {
		_, err := gate.ExecuteTool(context.Background(), agentcore.ToolCall{Name: name})
		assert.True(t, err != nil && strings.Contains(err.Error(), "需要许可："+name))
	}
	// The refusal matches the denial sentinel the observe layer keys on.
	_, err = gate.ExecuteTool(context.Background(), agentcore.ToolCall{Name: "mcp_write"})
	assert.True(t, errors.Is(err, tools.ErrDenied))
	assert.NoErr(t, gkconfig.Set("tools.auto_allow", []string{"mcp_write"}))
	gate, err = newGatedRegistry(registry, false)
	assert.NoErr(t, err)
	_, err = gate.ExecuteTool(context.Background(), agentcore.ToolCall{Name: "mcp_write"})
	assert.NoErr(t, err)
	_, err = gate.ExecuteTool(context.Background(), agentcore.ToolCall{Name: "gofer_work_report"})
	assert.True(t, err != nil)
}

// TestCLIMCPGateReadOnly verifies --read-only ignores user auto_allow extensions.
func TestCLIMCPGateReadOnly(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	registry := tools.NewRegistry()
	for _, name := range []string{"gofer_x_list", "gofer_work_report", "mcp_write", "gofer_issue_close"} {
		registry.Register(newServerStubTool("gofer", name))
	}
	assert.NoErr(t, gkconfig.Set("tools.auto_allow", []string{"mcp_write", "gofer_*"}))

	t.Run("read-only denies user-allowed write tools", func(t *testing.T) {
		gate, err := newGatedRegistry(registry, true)
		assert.NoErr(t, err)
		for _, name := range []string{"mcp_write", "gofer_issue_close"} {
			_, err := gate.ExecuteTool(context.Background(), agentcore.ToolCall{Name: name})
			assert.True(t, err != nil && strings.Contains(err.Error(), "需要许可："+name))
		}
	})
	t.Run("read-only allows default query tools and work report", func(t *testing.T) {
		gate, err := newGatedRegistry(registry, true)
		assert.NoErr(t, err)
		for _, name := range []string{"gofer_x_list", "gofer_work_report"} {
			_, err := gate.ExecuteTool(context.Background(), agentcore.ToolCall{Name: name})
			assert.NoErr(t, err)
		}
	})
	t.Run("non read-only keeps user allowlist", func(t *testing.T) {
		gate, err := newGatedRegistry(registry, false)
		assert.NoErr(t, err)
		_, err = gate.ExecuteTool(context.Background(), agentcore.ToolCall{Name: "mcp_write"})
		assert.NoErr(t, err)
	})
}

// TestCLIMCPGateServerQualified verifies the default allowlist trusts only the
// server named "gofer", so a third-party server cannot borrow a gofer tool name.
func TestCLIMCPGateServerQualified(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	registry := tools.NewRegistry()
	registry.Register(newServerStubTool("gofer", "gofer_work_report"))
	registry.Register(newServerStubTool("thirdparty", "gofer_session_list"))
	registry.Register(newServerStubTool("thirdparty", "gofer_issue_get"))

	run := func(t *testing.T, readOnly bool, name string) error {
		gate, err := newGatedRegistry(registry, readOnly)
		assert.NoErr(t, err)
		_, err = gate.ExecuteTool(context.Background(), agentcore.ToolCall{Name: name})
		return err
	}
	for _, readOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("readOnly=%v", readOnly), func(t *testing.T) {
			assert.NoErr(t, run(t, readOnly, "gofer_work_report"))
			for _, name := range []string{"gofer_session_list", "gofer_issue_get"} {
				err := run(t, readOnly, name)
				assert.True(t, err != nil && strings.Contains(err.Error(), "需要许可："+name))
			}
		})
	}
	t.Run("user entries: qualified is server-scoped, bare matches any server", func(t *testing.T) {
		assert.NoErr(t, gkconfig.Set("tools.auto_allow", []string{"thirdparty/gofer_session_list"}))
		assert.NoErr(t, run(t, false, "gofer_session_list"))
		assert.True(t, run(t, false, "gofer_issue_get") != nil)
		assert.True(t, run(t, false, "gofer_work_report") != nil)
		assert.NoErr(t, gkconfig.Set("tools.auto_allow", []string{"gofer_issue_get"}))
		assert.NoErr(t, run(t, false, "gofer_issue_get"))
	})
	t.Run("invalid server glob rejected", func(t *testing.T) {
		assert.NoErr(t, gkconfig.Set("tools.auto_allow", []string{"[/x"}))
		_, err := newGatedRegistry(registry, false)
		assert.True(t, err != nil)
	})
}
