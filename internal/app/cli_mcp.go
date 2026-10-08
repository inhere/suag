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
// It is also the only allowlist honored in --read-only mode. Entries are
// "server/toolglob": only the MCP server named "gofer" is trusted, so another
// server cannot get a default allow by exposing a same-named tool.
var defaultMCPAutoAllow = []string{
	"gofer/gofer_*_list", "gofer/gofer_*_get", "gofer/gofer_work_report", "gofer/gofer_session_list",
}

// mcpPatternMatches matches an allowlist entry against an MCP tool. An entry
// with "/" is "serverglob/toolglob" and needs the tool's server to match. A bare
// entry (legacy, user-configured tools.auto_allow only) matches the tool name
// from any server; prefer the server-qualified form.
func mcpPatternMatches(pattern, server, tool string) bool {
	if serverPat, toolPat, ok := strings.Cut(pattern, "/"); ok {
		if match, _ := path.Match(serverPat, server); !match {
			return false
		}
		match, _ := path.Match(toolPat, tool)
		return match
	}
	match, _ := path.Match(pattern, tool)
	return match
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
		serverPat, toolPat, qualified := strings.Cut(pattern, "/")
		if !qualified {
			toolPat = pattern
		} else if _, err := path.Match(serverPat, "sample"); err != nil {
			return nil, fmt.Errorf("tools.auto_allow glob %q: %w", pattern, err)
		}
		if _, err := path.Match(toolPat, "sample"); err != nil {
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

// Get passes tool lookup through so layers above (visibility, observe) can
// still resolve a tool's Kind and MCP server.
func (g *cliMCPGate) Get(name string) (tools.Tool, error) { return g.registry.Get(name) }

func (g *cliMCPGate) ExecuteTool(ctx context.Context, call agentcore.ToolCall) (*agentcore.ToolResult, error) {
	tool, err := g.registry.Get(call.Name)
	if err != nil {
		return nil, err
	}
	if tool.Kind() == tools.KindMCP {
		allowed := false
		server := ""
		if named, ok := tool.(tools.MCPServerNamer); ok {
			server = named.MCPServer()
		}
		for _, pattern := range g.patterns {
			if mcpPatternMatches(pattern, server, call.Name) {
				allowed = true
				break
			}
		}
		if !allowed {
			g.mu.Lock()
			g.denied = true
			g.deniedTool = call.Name
			g.mu.Unlock()
			return nil, tools.Denied(fmt.Sprintf("需要许可：%s，本次运行未授权", call.Name))
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
