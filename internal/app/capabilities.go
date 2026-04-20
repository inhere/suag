package app

import (
	gkconfig "github.com/gookit/config/v2"

	"github.com/inhere/axon/internal/capabilities/runtimecaps"
	"github.com/inhere/axon/internal/platform/tools"
	"github.com/inhere/axon/internal/platform/tools/native"
)

const nativeToolProviderID = "native"

type defaultCapabilities struct {
	Registry *tools.Registry
	Manager  *runtimecaps.CapabilityManager
}

// buildDefaultCapabilities creates the app's shared native tool wiring once so
// orchestrator, team, cron, and future capability gating all see the same base.
func buildDefaultCapabilities(workspace string) defaultCapabilities {
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
	return defaultCapabilities{
		Registry: registry,
		Manager:  manager,
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
