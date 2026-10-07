package app

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/inhere/suag/internal/acpserver"
	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/pkg/agentcore"
)

// RunPersonalACP serves the same persistent personal runner used by CLI over stdio.
func RunPersonalACP(ctx context.Context, input io.Reader, output io.Writer) error {
	return acpserver.Serve(ctx, input, output, func(ctx context.Context, cwd string, readOnly bool, servers []appconfig.MCPServerSettings, hook agentcore.Hook) (acpserver.Runtime, error) {
		workspace, err := filepath.Abs(cwd)
		if err != nil {
			return acpserver.Runtime{}, fmt.Errorf("resolve ACP workspace: %w", err)
		}
		var hooks []agentcore.Hook
		if hook != nil {
			hooks = []agentcore.Hook{hook}
		}
		runtime, err := openPersonalRuntimeConfigured(ctx, workspace, workspace, !readOnly, servers, hooks, personalOptions{})
		if err != nil {
			return acpserver.Runtime{}, err
		}
		return acpserver.Runtime{Runner: runtime.runner, Close: runtime.Close}, nil
	})
}
