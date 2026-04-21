package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/inhere/axon/internal/capabilities/runtimecaps"
	axconfig "github.com/inhere/axon/internal/config"
	"github.com/inhere/axon/internal/platform/tools"
)

type toolClient interface {
	Start(ctx context.Context) error
	ListTools(ctx context.Context) ([]mcpTool, error)
	AsTool(t mcpTool) tools.Tool
	Stop()
}

var newClient = func(cfg ServerConfig) toolClient {
	return NewClient(cfg)
}

// Provider exposes MCP tools as a runtime capability-governed tool source.
type Provider struct {
	id     string
	client toolClient
	tools  []tools.Tool
}

func NewProvider(ctx context.Context, settings axconfig.MCPServerSettings) (*Provider, error) {
	serverCfg := ServerConfig{
		Name:    settings.ID,
		Command: settings.Command,
		Args:    append([]string(nil), settings.Args...),
		Env:     formatEnvMap(settings.Env),
	}

	client := newClient(serverCfg)
	if err := client.Start(ctx); err != nil {
		return nil, fmt.Errorf("start mcp provider %q: %w", settings.ID, err)
	}

	defs, err := client.ListTools(ctx)
	if err != nil {
		client.Stop()
		return nil, fmt.Errorf("list mcp tools for %q: %w", settings.ID, err)
	}

	toolsList := make([]tools.Tool, 0, len(defs))
	for _, def := range defs {
		toolsList = append(toolsList, client.AsTool(def))
	}

	return &Provider{
		id:     providerID(settings.ID),
		client: client,
		tools:  toolsList,
	}, nil
}

func (p *Provider) ID() string {
	if p == nil {
		return ""
	}
	return p.id
}

func (p *Provider) ListToolRefs(ctx context.Context) ([]runtimecaps.ToolRef, error) {
	_ = ctx

	if p == nil || len(p.tools) == 0 {
		return nil, nil
	}

	refs := make([]runtimecaps.ToolRef, 0, len(p.tools))
	for _, tool := range p.tools {
		refs = append(refs, runtimecaps.ToolRef{
			Name:       tool.Name(),
			Source:     string(tools.KindMCP),
			ProviderID: p.id,
		})
	}
	return refs, nil
}

func (p *Provider) RegisterTools(registry *tools.Registry) error {
	if p == nil || registry == nil {
		return nil
	}

	for _, tool := range p.tools {
		registry.MustRegister(tool)
	}
	return nil
}

func (p *Provider) Close() error {
	if p == nil || p.client == nil {
		return nil
	}
	p.client.Stop()
	return nil
}

func providerID(serverID string) string {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		serverID = "default"
	}
	return "mcp." + serverID
}

func formatEnvMap(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}

	items := make([]string, 0, len(env))
	for key, value := range env {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		items = append(items, key+"="+strings.TrimSpace(value))
	}
	return items
}
