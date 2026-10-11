package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/inhere/suag/pkg/agentcore/api"
	"github.com/inhere/suag/pkg/agentcore/tools"
)

// ManifestSource produces the manifest value (any JSON-encodable value) on
// demand. The host injects it so this package never depends on wiring or the
// configuration layer.
type ManifestSource func(ctx context.Context) (any, error)

// ManifestTool (suag_manifest) reports what this suag instance can do: the
// model and tool set, memory backends, output limits. It is read-only and
// makes no network request.
type ManifestTool struct {
	source ManifestSource
}

// NewManifestTool creates the suag_manifest tool reading from source.
func NewManifestTool(source ManifestSource) *ManifestTool { return &ManifestTool{source: source} }

func (*ManifestTool) Name() string { return "suag_manifest" }

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
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}
}

func (t *ManifestTool) Execute(ctx context.Context, call api.ToolCall) (*api.ToolResult, error) {
	if t.source == nil {
		return nil, errors.New("suag_manifest: no manifest source configured")
	}
	v, err := t.source(ctx)
	if err != nil {
		return nil, fmt.Errorf("suag_manifest: %w", err)
	}
	var text string
	switch x := v.(type) {
	case string:
		text = x
	default:
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("suag_manifest: encode: %w", err)
		}
		text = string(b)
	}
	return &api.ToolResult{CallID: call.CallID, Name: t.Name(), Content: text}, nil
}
