package app

import (
	"context"
	"fmt"

	gkconfig "github.com/gookit/config/v2"

	"github.com/inhere/suag/internal/capability/runtimecaps"
	axconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/platform/tools"
	toolsmcp "github.com/inhere/suag/internal/platform/tools/mcp"
	"github.com/inhere/suag/internal/platform/tools/native"
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
	for _, tool := range native.NewFileTools(workspace) {
		registerNativeTool(registry, &toolRefs, tool)
	}
	registerNativeTool(registry, &toolRefs, native.NewShellExecTool(workspace, 0))

	if serpKey := gkconfig.String("serpapi_key"); serpKey != "" {
		registerNativeTool(registry, &toolRefs, native.NewWebSearchTool(serpKey))
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
