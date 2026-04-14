package native

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/inhere/axon/internal/platform/llm"
	"github.com/inhere/axon/internal/platform/tools"
	"github.com/inhere/axon/pkg/agentcore/api"
)

const defaultExecTimeout = 30 * time.Second

// ShellExecTool runs a shell command inside the workspace directory.
// Commands are time-limited to prevent runaway processes.
type ShellExecTool struct {
	workspace string
	timeout   time.Duration
}

// NewShellExecTool returns a ShellExecTool scoped to workspace.
func NewShellExecTool(workspace string, timeout time.Duration) *ShellExecTool {
	if timeout <= 0 {
		timeout = defaultExecTimeout
	}
	return &ShellExecTool{workspace: workspace, timeout: timeout}
}

func (t *ShellExecTool) Name() string     { return "shell_exec" }
func (t *ShellExecTool) Kind() tools.Kind { return tools.KindNative }
func (t *ShellExecTool) Description() string {
	return "Execute a shell command in the workspace directory. Returns stdout+stderr. Max " + t.timeout.String() + " timeout."
}
func (t *ShellExecTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: llm.SchemaProperty{
			Type: "object",
			Properties: map[string]*llm.SchemaProperty{
				"command": {Type: "string", Description: "Shell command to execute"},
			},
			Required: []string{"command"},
		},
	}
}

func (t *ShellExecTool) Execute(ctx context.Context, call api.ToolCall) (*api.ToolResult, error) {
	command, _ := call.Arguments["command"].(string)
	if command == "" {
		return nil, fmt.Errorf("shell_exec: command is required")
	}

	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	// Use sh -c on Unix, cmd /C on Windows — both are handled by exec.CommandContext.
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = t.workspace

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	var sb strings.Builder
	if stdout.Len() > 0 {
		sb.WriteString(stdout.String())
	}
	if stderr.Len() > 0 {
		sb.WriteString("\n[stderr]\n")
		sb.WriteString(stderr.String())
	}
	output := strings.TrimSpace(sb.String())

	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("shell_exec: timeout after %s", t.timeout)
		}
		return nil, fmt.Errorf("shell_exec exit %w: %s", err, output)
	}
	return &api.ToolResult{
		CallID:  call.CallID,
		Name:    t.Name(),
		Content: output,
	}, nil
}
