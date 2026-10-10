package axconfig

import (
	"encoding/json"
	"fmt"
	"github.com/inhere/suag/internal/platform/tools/mcp"
	"os"
	"sort"
	"strings"
)

// ReadClaudeMCPConfigs reads repeatable Claude-style stdio MCP configuration files.
func ReadClaudeMCPConfigs(paths []string) ([]mcp.ServerSettings, error) {
	var servers []mcp.ServerSettings
	seen := make(map[string]bool)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read MCP config %q: %w", path, err)
		}
		var config struct {
			Servers map[string]json.RawMessage `json:"mcpServers"`
		}
		if err := json.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("parse MCP config %q: %w", path, err)
		}
		if config.Servers == nil {
			return nil, fmt.Errorf("MCP config %q requires mcpServers", path)
		}
		names := make([]string, 0, len(config.Servers))
		for name := range config.Servers {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			var entry struct {
				Command string            `json:"command"`
				Args    []string          `json:"args"`
				Env     map[string]string `json:"env"`
				Type    string            `json:"type"`
				URL     string            `json:"url"`
			}
			if err := json.Unmarshal(config.Servers[name], &entry); err != nil {
				return nil, fmt.Errorf("MCP server %q in %q: %w", name, path, err)
			}
			if strings.TrimSpace(name) == "" || strings.TrimSpace(entry.Command) == "" || entry.URL != "" || (entry.Type != "" && entry.Type != "stdio") {
				return nil, fmt.Errorf("MCP server %q in %q requires stdio command", name, path)
			}
			if seen[name] {
				return nil, fmt.Errorf("duplicate MCP server %q", name)
			}
			seen[name] = true
			servers = append(servers, mcp.ServerSettings{ID: name, Command: entry.Command, Args: entry.Args, Env: entry.Env})
		}
	}
	return servers, nil
}
