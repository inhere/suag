package app

import (
	"context"
	"fmt"
	"path"
	"strings"
	"sync"

	gkconfig "github.com/gookit/config/v2"
	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/pkg/agentcore"
)

// defaultMCPAutoAllow is the built-in MCP allowlist: query tools plus gofer_work_report.
// It is also the only allowlist honored in --read-only mode.
var defaultMCPAutoAllow = []string{
	"gofer_*_list", "gofer_*_get", "gofer_work_report", "gofer_session_list",
}

// cliMCPGate checks the tool kind at dispatch, so --allow-write never grants MCP writes.
type cliMCPGate struct {
	agentcore.NopHook
	registry   *tools.Registry
	patterns   []string
	mu         sync.Mutex
	denied     bool
	deniedTool string
	usage      agentcore.Usage
}

// newCLIMCPGate builds the dispatch gate. In readOnly mode the user's
// tools.auto_allow is still validated but never widens the allowlist: only
// defaultMCPAutoAllow applies, so no extra (possibly mutating) MCP tool runs.
func newCLIMCPGate(registry *tools.Registry, readOnly bool) (*cliMCPGate, error) {
	patterns := append([]string(nil), defaultMCPAutoAllow...)
	if raw := gkconfig.Get("tools.auto_allow"); raw != nil {
		patterns = nil
		switch value := raw.(type) {
		case []string:
			patterns = append(patterns, value...)
		case []any:
			for _, item := range value {
				pattern, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("tools.auto_allow must contain glob strings")
				}
				patterns = append(patterns, pattern)
			}
		default:
			return nil, fmt.Errorf("tools.auto_allow must be a list of glob strings")
		}
	}
	for _, pattern := range patterns {
		if strings.TrimSpace(pattern) == "" {
			return nil, fmt.Errorf("tools.auto_allow contains an empty glob")
		}
		if _, err := path.Match(pattern, "sample"); err != nil {
			return nil, fmt.Errorf("tools.auto_allow glob %q: %w", pattern, err)
		}
	}
	if readOnly {
		patterns = append([]string(nil), defaultMCPAutoAllow...)
	}
	return &cliMCPGate{registry: registry, patterns: patterns}, nil
}

func (g *cliMCPGate) ListTools(ctx context.Context) ([]agentcore.ToolSchema, error) {
	return g.registry.ListTools(ctx)
}

func (g *cliMCPGate) ExecuteTool(ctx context.Context, call agentcore.ToolCall) (*agentcore.ToolResult, error) {
	tool, err := g.registry.Get(call.Name)
	if err != nil {
		return nil, err
	}
	if tool.Kind() == tools.KindMCP {
		allowed := false
		for _, pattern := range g.patterns {
			if match, _ := path.Match(pattern, call.Name); match {
				allowed = true
				break
			}
		}
		if !allowed {
			g.mu.Lock()
			g.denied = true
			g.deniedTool = call.Name
			g.mu.Unlock()
			return nil, fmt.Errorf("需要许可：%s，本次运行未授权", call.Name)
		}
	}
	return g.registry.ExecuteTool(ctx, call)
}

// AfterModel ends a headless turn if the model repeats a denied MCP request.
// The first denial is already in the model's context as a tool error.
func (g *cliMCPGate) AfterModel(_ context.Context, event *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	if event == nil || event.Result == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.usage.InputTokens += event.Result.Usage.InputTokens
	g.usage.OutputTokens += event.Result.Usage.OutputTokens
	g.usage.TotalTokens += event.Result.Usage.TotalTokens
	g.usage.CacheReadTokens += event.Result.Usage.CacheReadTokens
	g.usage.CacheWriteTokens += event.Result.Usage.CacheWriteTokens
	g.usage.CostUSD += event.Result.Usage.CostUSD
	if g.denied && len(event.Result.ToolCalls) > 0 {
		output := strings.TrimSpace(event.Result.Content)
		if output == "" {
			output = fmt.Sprintf("需要许可：%s，本次运行未授权", g.deniedTool)
		}
		ctrl.ShortCircuit(&agentcore.RunResult{Status: agentcore.RunCompleted, Output: output, Usage: g.usage})
	}
	return nil
}

func (g *cliMCPGate) WasDenied() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.denied
}

func (g *cliMCPGate) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.denied = false
	g.deniedTool = ""
	g.usage = agentcore.Usage{}
}
