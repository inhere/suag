package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/gookit/goutil/cflag/capp"
	"github.com/inhere/suag/internal/acpserver"
	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/internal/wiring"
	"github.com/inhere/suag/pkg/agentcore"
)

func newACPCommand(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) *capp.Cmd {
	acp := capp.NewCmd("acp", "Serve an ACP session over stdio", func(c *capp.Cmd) error {
		if len(c.RemainArgs()) != 0 {
			return fmt.Errorf("suag acp does not accept arguments")
		}
		if err := RunPersonalACP(ctx, stdin, stdout); err != nil {
			return fmt.Errorf("acp: %w", err)
		}
		return nil
	})
	acp.SetOutput(stderr)
	return acp
}

// RunPersonalACP serves the same persistent personal runner used by CLI over stdio.
func RunPersonalACP(ctx context.Context, input io.Reader, output io.Writer) error {
	return acpserver.Serve(ctx, input, output, func(ctx context.Context, cwd string, readOnly bool, servers []appconfig.MCPServerSettings, gate acpserver.TurnGate) (acpserver.Runtime, error) {
		workspace, err := filepath.Abs(cwd)
		if err != nil {
			return acpserver.Runtime{}, fmt.Errorf("resolve ACP workspace: %w", err)
		}
		// The gate is both the notification hook and the approval policy. A nil
		// gate (session create/load, no turn) leaves the approver nil, which the
		// approval layer treats as deny-all.
		var hooks []agentcore.Hook
		var approver tools.Approver
		if gate != nil {
			hooks, approver = []agentcore.Hook{gate}, gate
		}
		runtime, err := wiring.Open(ctx, wiring.Options{Workspace: workspace, RuleCWD: workspace, AllowWrite: !readOnly, Servers: servers, Hooks: hooks, Approver: approver})
		if err != nil {
			return acpserver.Runtime{}, err
		}
		registry := runtime.Registry
		isMCP := func(name string) bool {
			tool, err := registry.Get(name)
			return err == nil && tool.Kind() == tools.KindMCP
		}
		return acpserver.Runtime{Runner: runtime.Runner, Close: runtime.Close, IsMCPTool: isMCP, ContextWindow: runtime.ContextWindow}, nil
	})
}
