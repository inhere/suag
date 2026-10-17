package wiring

import (
	"os/exec"
	"sort"
	"strings"

	appconfig "github.com/inhere/suga/internal/config"
	"github.com/inhere/suga/pkg/agentcore/tools"
	"github.com/inhere/suga/pkg/agentcore/tools/mcp"
)

const (
	preferredReasonFoundPath        = "found on PATH"
	preferredReasonMissingPath      = "not found on PATH"
	preferredReasonMCPNotConfigured = "server not configured"
	preferredReasonMCPConnected     = "connected at runtime"
	preferredReasonMCPUnavailable   = "not connected at runtime"
)

// PreferredToolStatus is the safe runtime fact used by later prompt and
// manifest layers. It intentionally carries no command path, MCP arguments, or
// environment values.
type PreferredToolStatus struct {
	Name    string
	Kind    string
	When    string
	Present bool
	Reason  string
}

// ResolvePreferredTools validates the configured declarations and resolves
// their current availability without executing a command. MCP availability is
// inferred from the already-built registry, so this function never starts a
// second provider or changes provider lifecycle.
func ResolvePreferredTools(registry *tools.Registry, extraServers []mcp.ServerSettings) ([]PreferredToolStatus, error) {
	preferred, err := appconfig.ReadPreferredTools()
	if err != nil {
		return nil, err
	}
	servers := append(appconfig.ReadMCPServerSettings(), extraServers...)
	configured := make(map[string]struct{}, len(servers))
	for _, server := range servers {
		id := strings.TrimSpace(server.ID)
		if id == "" {
			id = strings.TrimSpace(server.Command)
		}
		if id != "" {
			configured[id] = struct{}{}
		}
	}

	statuses := make([]PreferredToolStatus, 0, len(preferred))
	for _, item := range preferred {
		status := PreferredToolStatus{Name: item.Name, Kind: item.Kind, When: item.When}
		switch item.Kind {
		case "command":
			if _, err := exec.LookPath(item.Name); err == nil {
				status.Present = true
				status.Reason = preferredReasonFoundPath
			} else {
				status.Reason = preferredReasonMissingPath
			}
		case "mcp":
			if _, ok := configured[item.Name]; !ok {
				status.Reason = preferredReasonMCPNotConfigured
				break
			}
			if registry != nil && registryHasMCPServer(registry, item.Name) {
				status.Present = true
				status.Reason = preferredReasonMCPConnected
			} else {
				status.Reason = preferredReasonMCPUnavailable
			}
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// refreshPreferredCommands rechecks only command declarations at a segment
// boundary. MCP statuses stay frozen for the runtime, as required by the
// simplified design; no provider is restarted or probed here.
func refreshPreferredCommands(statuses []PreferredToolStatus) []PreferredToolStatus {
	refreshed := append([]PreferredToolStatus(nil), statuses...)
	for i := range refreshed {
		if refreshed[i].Kind != "command" {
			continue
		}
		if _, err := exec.LookPath(refreshed[i].Name); err == nil {
			refreshed[i].Present = true
			refreshed[i].Reason = preferredReasonFoundPath
		} else {
			refreshed[i].Present = false
			refreshed[i].Reason = preferredReasonMissingPath
		}
	}
	return refreshed
}

// preferredMCPToolNames returns every registered tool belonging to a preferred
// MCP server. Pinning is by tool name at the Visibility boundary, while the
// resolver remains concerned only with safe server availability facts.
func preferredMCPToolNames(registry *tools.Registry, statuses []PreferredToolStatus) []string {
	if registry == nil {
		return nil
	}
	servers := make(map[string]bool)
	for _, status := range statuses {
		if status.Kind == "mcp" && status.Present {
			servers[status.Name] = true
		}
	}
	var names []string
	for _, name := range registry.List() {
		tool, err := registry.Get(name)
		if err == nil && tools.IsMCP(tool) && servers[tools.MCPServerOf(tool)] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func registryHasMCPServer(registry *tools.Registry, serverID string) bool {
	for _, name := range registry.List() {
		tool, err := registry.Get(name)
		if err == nil && tools.IsMCP(tool) && tools.MCPServerOf(tool) == serverID {
			return true
		}
	}
	return false
}
