package mcp

import (
	"context"
	"errors"
	"fmt"
	"github.com/inhere/suag/pkg/agentcore/api"
	"log/slog"
	"strings"
	"time"

	axconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/platform/tools"
)

type toolClient interface {
	Start(ctx context.Context) error
	ListTools(ctx context.Context) ([]mcpTool, error)
	AsTool(t mcpTool) api.Tool
	Stop()
}

var newClient = func(cfg ServerConfig) toolClient {
	return NewClient(cfg)
}

// startupTimeout bounds MCP server spawn, initialize and tools/list. It is a var so tests can shrink it.
var startupTimeout = 30 * time.Second

// startErr turns a bare context error into an actionable message.
func startErr(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) && errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("timed out after %s waiting for server: %w", startupTimeout, err)
	}
	return err
}

// Provider exposes MCP tools as a runtime capability-governed tool source.
type Provider struct {
	id     string
	client toolClient
	tools  []api.Tool
}

// NewProviderForTools constructs a Provider from already-materialized MCP tools.
// It exists for higher-level wiring tests so app capability assembly can verify
// provider composition without starting a real MCP subprocess.
func NewProviderForTools(id string, toolsList []api.Tool, client toolClient) *Provider {
	return &Provider{
		id:     providerID(id),
		client: client,
		tools:  append([]api.Tool(nil), toolsList...),
	}
}

func NewProvider(ctx context.Context, settings axconfig.MCPServerSettings) (*Provider, error) {
	serverCfg := ServerConfig{
		Name:    settings.ID,
		Command: settings.Command,
		Args:    append([]string(nil), settings.Args...),
		Env:     formatEnvMap(settings.Env),

		CallTimeout: axconfig.MCPCallTimeout(),
	}

	// Startup (spawn + initialize + tools/list) is bounded and honors the caller's
	// ctx, so a broken server can never hang `suag run`.
	startCtx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	client := newClient(serverCfg)
	if err := client.Start(startCtx); err != nil {
		client.Stop() // Start already cleans up; Stop is idempotent
		return nil, fmt.Errorf("start mcp provider %q: %w", settings.ID, startErr(startCtx, err))
	}

	defs, err := client.ListTools(startCtx)
	if err != nil {
		client.Stop()
		return nil, fmt.Errorf("list mcp tools for %q: %w", settings.ID, startErr(startCtx, err))
	}

	toolsList := make([]api.Tool, 0, len(defs))
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

func (p *Provider) RegisterTools(registry *tools.Registry) error {
	if p == nil || registry == nil {
		return nil
	}

	// First registration wins: built-in tools and earlier servers keep their
	// names, so a later server cannot impersonate them. Callers register
	// providers in configuration order, which keeps the result reproducible.
	for _, tool := range p.tools {
		if !registry.TryRegister(tool) {
			slog.Warn("mcp tool dropped: name already registered", "server", p.id, "tool", tool.Name())
		}
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
