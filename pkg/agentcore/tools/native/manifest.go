package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/inhere/suga/pkg/agentcore/api"
	"github.com/inhere/suga/pkg/agentcore/tools"
)

// ManifestSource produces the manifest value (any JSON-encodable value) on
// demand. The host injects it so this package never depends on wiring or the
// configuration layer.
type ManifestSource func(ctx context.Context) (any, error)

// ManifestTool (suga_manifest) reports what this suga instance can do: the
// model and tool set, memory backends, output limits. It is read-only and
// makes no network request.
type ManifestTool struct {
	source ManifestSource
}

// NewManifestTool creates the suga_manifest tool reading from source.
func NewManifestTool(source ManifestSource) *ManifestTool { return &ManifestTool{source: source} }

func (*ManifestTool) Name() string { return "suga_manifest" }

func (*ManifestTool) Description() string {
	return "Show this agent's capabilities as JSON: version, models, tools with their metadata, MCP servers, " +
		"skills, memory backends per scope, output limits and write policy. Read-only; no network access."
}

// ToolMeta marks the tool read-only: the data is local configuration.
func (*ManifestTool) ToolMeta() tools.Meta {
	return tools.Meta{Category: tools.CategoryRead, ReadOnly: true}
}

func (t *ManifestTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		// An optional parameter keeps "properties" non-empty: some
		// OpenAI-compatible gateways turn an empty properties object into
		// null and reject the whole request.
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"section": map[string]any{
					"type":        "string",
					"description": "Optional top-level section to return, e.g. models, tools, memory, output; empty returns everything",
				},
			},
		},
	}
}

func (t *ManifestTool) Execute(ctx context.Context, call api.ToolCall) (*api.ToolResult, error) {
	if t.source == nil {
		return nil, errors.New("suga_manifest: no manifest source configured")
	}
	v, err := t.source(ctx)
	if err != nil {
		return nil, fmt.Errorf("suga_manifest: %w", err)
	}
	section, _ := call.Arguments["section"].(string)
	section = strings.TrimSpace(section)
	var text string
	switch x := v.(type) {
	case string:
		if section != "" {
			return nil, errors.New("suga_manifest: section is not supported by this manifest source")
		}
		text = x
	default:
		if section != "" {
			// Round-trip through JSON so the section names match the keys
			// the full output shows.
			var all map[string]any
			b, err := json.Marshal(v)
			if err == nil {
				err = json.Unmarshal(b, &all)
			}
			if err != nil {
				return nil, fmt.Errorf("suga_manifest: encode: %w", err)
			}
			part, ok := all[section]
			if !ok {
				keys := make([]string, 0, len(all))
				for k := range all {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				return nil, fmt.Errorf("suga_manifest: unknown section %q (have: %s)", section, strings.Join(keys, ", "))
			}
			v = part
		}
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("suga_manifest: encode: %w", err)
		}
		text = string(b)
	}
	return &api.ToolResult{CallID: call.CallID, Name: t.Name(), Content: text}, nil
}
