package axconfig

import (
	"strings"

	gkconfig "github.com/gookit/config/v2"
)

// MCPServerSettings is the normalized stdio MCP server config used by the app
// capability builder.
type MCPServerSettings struct {
	ID      string
	Command string
	Args    []string
	Env     map[string]string
}

func ReadMCPServerSettings() []MCPServerSettings {
	raw := gkconfig.Get("mcp.servers")
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		typedItems, ok := raw.([]map[string]any)
		if !ok || len(typedItems) == 0 {
			return nil
		}

		items = make([]any, 0, len(typedItems))
		for _, item := range typedItems {
			items = append(items, item)
		}
	}

	servers := make([]MCPServerSettings, 0, len(items))
	for _, item := range items {
		server, ok := normalizeMCPServerSettings(item)
		if !ok {
			continue
		}
		servers = append(servers, server)
	}
	return servers
}

func normalizeMCPServerSettings(raw any) (MCPServerSettings, bool) {
	item, ok := raw.(map[string]any)
	if !ok {
		return MCPServerSettings{}, false
	}

	command := strings.TrimSpace(stringValue(item["command"]))
	if command == "" {
		return MCPServerSettings{}, false
	}

	id := strings.TrimSpace(stringValue(item["id"]))
	if id == "" {
		id = command
	}

	return MCPServerSettings{
		ID:      id,
		Command: command,
		Args:    normalizeStringList(item["args"]),
		Env:     normalizeStringMap(item["env"]),
	}, true
}

func normalizeStringList(raw any) []string {
	switch value := raw.(type) {
	case []string:
		items := make([]string, 0, len(value))
		for _, item := range value {
			items = append(items, strings.TrimSpace(item))
		}
		return items
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			items = append(items, strings.TrimSpace(stringValue(item)))
		}
		return items
	default:
		return nil
	}
}

func normalizeStringMap(raw any) map[string]string {
	value, ok := raw.(map[string]any)
	if !ok || len(value) == 0 {
		return nil
	}

	items := make(map[string]string, len(value))
	for key, val := range value {
		items[strings.TrimSpace(key)] = strings.TrimSpace(stringValue(val))
	}
	return items
}

func stringValue(raw any) string {
	switch value := raw.(type) {
	case string:
		return value
	default:
		return ""
	}
}
