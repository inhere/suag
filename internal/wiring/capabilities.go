package wiring

import (
	"context"
	"fmt"
	"time"

	gkconfig "github.com/gookit/config/v2"

	"github.com/inhere/suag/internal/capability/runtimecaps"
	axconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/platform/tools"
	toolsmcp "github.com/inhere/suag/internal/platform/tools/mcp"
	"github.com/inhere/suag/internal/platform/tools/native"
	"github.com/inhere/suag/pkg/agentcore"
)

const nativeToolProviderID = "native"

type capabilityToolProvider interface {
	runtimecaps.ToolProvider
	RegisterTools(registry *tools.Registry) error
	Close() error
}

var mcpProviderFactory = func(ctx context.Context, settings axconfig.MCPServerSettings) (capabilityToolProvider, error) {
	return toolsmcp.NewProvider(ctx, settings)
}

type defaultCapabilities struct {
	Registry *tools.Registry
	Manager  *runtimecaps.CapabilityManager
	closers  []capabilityToolProvider
}

// buildDefaultCapabilities assembles native and configured MCP tools for the
// personal Agent's writable mode.
func buildDefaultCapabilities(workspace string) (defaultCapabilities, error) {
	return buildDefaultCapabilitiesWithServers(context.Background(), workspace, nil)
}

func buildDefaultCapabilitiesWithServers(ctx context.Context, workspace string, additional []axconfig.MCPServerSettings) (defaultCapabilities, error) {
	registry := tools.NewRegistry()
	manager := runtimecaps.NewManager()

	toolRefs := make([]runtimecaps.ToolRef, 0, 5)
	for _, tool := range native.BuildTools(nativeEnv(workspace), true) {
		registerNativeTool(registry, &toolRefs, tool)
	}

	manager.MustRegisterToolProvider(runtimecaps.NewStaticToolProvider(nativeToolProviderID, toolRefs))
	built := defaultCapabilities{
		Registry: registry,
		Manager:  manager,
	}
	servers := append(axconfig.ReadMCPServerSettings(), additional...)
	for _, settings := range servers {
		provider, err := mcpProviderFactory(ctx, settings)
		if err != nil {
			_ = built.Close()
			return defaultCapabilities{}, fmt.Errorf("build mcp provider %q: %w", settings.ID, err)
		}
		if err := provider.RegisterTools(registry); err != nil {
			_ = provider.Close()
			_ = built.Close()
			return defaultCapabilities{}, fmt.Errorf("register mcp tools for %q: %w", settings.ID, err)
		}
		manager.MustRegisterToolProvider(provider)
		built.closers = append(built.closers, provider)
	}
	return built, nil
}

func (c defaultCapabilities) Close() error {
	var firstErr error
	for _, closer := range c.closers {
		if closer == nil {
			continue
		}
		if err := closer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// nativeEnv reads the tools.* settings native tools are built with.
func nativeEnv(workspace string) native.Env {
	return native.Env{
		Workspace:         workspace,
		MaxOutputBytes:    gkconfig.Int("tools.max_output_bytes", native.DefaultMaxOutputBytes),
		SerpAPIKey:        gkconfig.String("serpapi_key"),
		Shell:             gkconfig.String("tools.shell"),
		FetchAllowPrivate: gkconfig.Bool("tools.web_fetch.allow_private"),
		ShellTimeoutMax: time.Duration(gkconfig.Int("tools.shell_timeout_max",
			int(native.DefaultShellTimeoutMax/time.Second))) * time.Second,
	}
}

func registerNativeTool(registry *tools.Registry, refs *[]runtimecaps.ToolRef, tool tools.Tool) {
	registry.MustRegister(tool)
	*refs = append(*refs, runtimecaps.ToolRef{
		Name:       tool.Name(),
		Source:     string(tool.Kind()),
		ProviderID: nativeToolProviderID,
	})
}

func swapMCPProviderFactoryForTest(factory func(ctx context.Context, settings axconfig.MCPServerSettings) (capabilityToolProvider, error)) func() {
	prev := mcpProviderFactory
	mcpProviderFactory = factory
	return func() {
		mcpProviderFactory = prev
	}
}

// newToolVisibility wraps the (approval-gated) registry in the deferred-tool
// layer per tools.deferred. When tools end up deferred, tool_search is added to
// base so the model can load them; otherwise it is not registered at all.
func newToolVisibility(base *tools.Registry, inner agentcore.ToolRegistry, contextWindow int) (*tools.Visibility, error) {
	mode, err := tools.ParseDeferredMode(gkconfig.String("tools.deferred"))
	if err != nil {
		return nil, err
	}
	lookup, ok := inner.(interface {
		agentcore.ToolRegistry
		tools.Lookup
	})
	if !ok {
		return nil, fmt.Errorf("tool registry %T does not support lookup", inner)
	}
	vis := tools.NewVisibility(lookup, tools.VisibilityOptions{Mode: mode, ContextWindow: contextWindow})
	if vis.Active() {
		base.MustRegister(native.NewToolSearchTool(vis))
	}
	return vis, nil
}
