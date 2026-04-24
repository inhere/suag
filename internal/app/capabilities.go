package app

import (
	"context"
	"fmt"

	gkconfig "github.com/gookit/config/v2"

	"github.com/inhere/axon/internal/capability/runtimecaps"
	axconfig "github.com/inhere/axon/internal/config"
	"github.com/inhere/axon/internal/platform/tools"
	toolsmcp "github.com/inhere/axon/internal/platform/tools/mcp"
	"github.com/inhere/axon/internal/platform/tools/native"
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

// buildDefaultCapabilities creates the app's shared native tool wiring once so
// orchestrator, team, cron, and future capability gating all see the same base.
func buildDefaultCapabilities(workspace string) (defaultCapabilities, error) {
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
	for _, settings := range axconfig.ReadMCPServerSettings() {
		provider, err := mcpProviderFactory(context.Background(), settings)
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
