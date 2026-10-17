package axconfig

import (
	"fmt"
	"strings"

	goccyyaml "github.com/goccy/go-yaml"
	gkconfig "github.com/gookit/config/v2"
)

// PreferredTool is one user-declared command or MCP server to favor when it is
// available. It contains only an identifier and a human-readable use case.
type PreferredTool struct {
	Name string `yaml:"name" json:"name"`
	Kind string `yaml:"kind" json:"kind"`
	When string `yaml:"when" json:"when"`
}

// ReadPreferredTools reads and validates the complete tools.preferred value.
// Runtime discovery is deliberately outside this reader: config validation must
// not execute commands or start an MCP server.
func ReadPreferredTools() ([]PreferredTool, error) {
	raw := gkconfig.Get("tools.preferred")
	if raw == nil {
		return []PreferredTool{}, nil
	}
	encoded, err := goccyyaml.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("tools.preferred must be a list of mappings: %w", err)
	}
	var items []PreferredTool
	if err := goccyyaml.UnmarshalWithOptions(encoded, &items, goccyyaml.DisallowUnknownField()); err != nil {
		return nil, fmt.Errorf("tools.preferred must be a list of mappings: %w", err)
	}
	seen := make(map[string]struct{}, len(items))
	for i := range items {
		item := &items[i]
		item.Name = strings.TrimSpace(item.Name)
		item.Kind = strings.ToLower(strings.TrimSpace(item.Kind))
		item.When = strings.TrimSpace(item.When)
		if item.Name == "" || strings.IndexFunc(item.Name, func(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' }) >= 0 {
			return nil, fmt.Errorf("tools.preferred[%d].name must be a non-empty name without arguments", i)
		}
		if item.When == "" {
			return nil, fmt.Errorf("tools.preferred[%d].when must be non-empty", i)
		}
		if item.Kind != "command" && item.Kind != "mcp" {
			return nil, fmt.Errorf("tools.preferred[%d].kind must be command or mcp", i)
		}
		if _, ok := seen[item.Name]; ok {
			return nil, fmt.Errorf("tools.preferred contains duplicate name %q", item.Name)
		}
		seen[item.Name] = struct{}{}
	}
	return items, nil
}
