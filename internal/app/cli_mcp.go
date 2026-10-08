package app

import (
	"context"
	"fmt"
	"path"
	"strings"

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

// cliMCPGate is the CLI tools.Approver: it checks the tool kind at dispatch, so
// --allow-write never grants MCP writes. Only MCP tools are gated; which native
// tools exist at all (--read-only, --allow-write) is decided by the tool set.
type cliMCPGate struct {
	patterns []string
}

// newCLIMCPGate builds the dispatch approver. In readOnly mode the user's
// tools.auto_allow is still validated but never widens the allowlist: only
// defaultMCPAutoAllow applies, so no extra (possibly mutating) MCP tool runs.
func newCLIMCPGate(readOnly bool) (*cliMCPGate, error) {
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
	return &cliMCPGate{patterns: patterns}, nil
}

// Decide allows native tools and allowlisted MCP tools; any other MCP tool is
// refused with tools.Denied, which the model sees as a tool error.
func (g *cliMCPGate) Decide(_ context.Context, call agentcore.ToolCall, tool tools.Tool) error {
	if tool.Kind() != tools.KindMCP {
		return nil
	}
	server := ""
	if named, ok := tool.(tools.MCPServerNamer); ok {
		server = named.MCPServer()
	}
	for _, pattern := range g.patterns {
		if mcpPatternMatches(pattern, server, call.Name) {
			return nil
		}
	}
	return tools.Denied(fmt.Sprintf("需要许可：%s，本次运行未授权", call.Name))
}
