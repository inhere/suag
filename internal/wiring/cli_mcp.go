package wiring

import (
	"context"
	"fmt"
	"path"
	"strings"

	gkconfig "github.com/gookit/config/v2"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/tools"
)

// mcpPatternMatches matches an allowlist entry against an MCP tool. An entry
// with "/" is "serverglob/toolglob" and needs the tool's server to match. A bare
// entry (tools.auto_allow only) matches the tool name from any server; prefer
// the server-qualified form.
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

// newCLIMCPGate builds the dispatch approver. There is no built-in allowlist:
// an unconfigured list denies every MCP tool.
//   - Normal mode uses tools.auto_allow (bare or server-qualified globs).
//   - readOnly mode uses only tools.read_only_allow, whose entries must be
//     server-qualified ("server/toolglob") so a same-named tool from another
//     server is never trusted. tools.auto_allow is still validated there but
//     never widens the allowlist, so no extra (possibly mutating) MCP tool runs.
func newCLIMCPGate(readOnly bool) (*cliMCPGate, error) {
	autoAllow, err := mcpAllowlist("tools.auto_allow", false)
	if err != nil {
		return nil, err
	}
	readOnlyAllow, err := mcpAllowlist("tools.read_only_allow", true)
	if err != nil {
		return nil, err
	}
	if readOnly {
		return &cliMCPGate{patterns: readOnlyAllow}, nil
	}
	return &cliMCPGate{patterns: autoAllow}, nil
}

// mcpAllowlist reads and validates one allowlist config key. qualifiedOnly
// rejects entries without a "server/" part.
func mcpAllowlist(key string, qualifiedOnly bool) ([]string, error) {
	raw := gkconfig.Get(key)
	if raw == nil {
		return nil, nil
	}
	var patterns []string
	switch value := raw.(type) {
	case []string:
		patterns = append(patterns, value...)
	case []any:
		for _, item := range value {
			pattern, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s must contain glob strings", key)
			}
			patterns = append(patterns, pattern)
		}
	default:
		return nil, fmt.Errorf("%s must be a list of glob strings", key)
	}
	for _, pattern := range patterns {
		if strings.TrimSpace(pattern) == "" {
			return nil, fmt.Errorf("%s contains an empty glob", key)
		}
		serverPat, toolPat, qualified := strings.Cut(pattern, "/")
		if !qualified {
			if qualifiedOnly {
				return nil, fmt.Errorf("%s entry %q must be server-qualified (server/toolglob)", key, pattern)
			}
			toolPat = pattern
		} else if _, err := path.Match(serverPat, "sample"); err != nil {
			return nil, fmt.Errorf("%s glob %q: %w", key, pattern, err)
		}
		if _, err := path.Match(toolPat, "sample"); err != nil {
			return nil, fmt.Errorf("%s glob %q: %w", key, pattern, err)
		}
	}
	return patterns, nil
}

// Decide allows native tools and allowlisted MCP tools; any other MCP tool is
// refused with tools.Denied, which the model sees as a tool error.
func (g *cliMCPGate) Decide(_ context.Context, call agentcore.ToolCall, tool agentcore.Tool) error {
	if tools.KindOf(tool) != tools.KindMCP {
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
