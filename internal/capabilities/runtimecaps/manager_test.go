package runtimecaps

import (
	"context"
	"testing"

	"github.com/gookit/goutil/testutil/assert"
)

func TestCapabilityManager_BuildDefaultSetIncludesRegisteredToolRefs(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	provider := NewStaticToolProvider("native", []ToolRef{
		{Name: "file_read", Source: "native", ProviderID: "native"},
		{Name: "shell_exec", Source: "native", ProviderID: "native"},
	})

	manager.MustRegisterToolProvider(provider)

	set := manager.BuildDefaultCapabilitySet(BuildSetInput{})

	assert.Eq(t, 2, len(set.Tools))
	assert.Eq(t, "file_read", set.Tools[0].Name)
	assert.Eq(t, "native", set.Tools[0].Source)
	assert.Eq(t, "native", set.Tools[0].ProviderID)
	assert.Eq(t, "shell_exec", set.Tools[1].Name)
}

func TestCapabilityManager_ResolveTurnCapabilitiesIncludesToolNames(t *testing.T) {
	t.Parallel()

	manager := NewManager()

	resolved := manager.ResolveTurnCapabilities(CapabilitySet{
		Tools: []ToolRef{
			{Name: "file_read", Source: "native", ProviderID: "native"},
			{Name: "shell_exec", Source: "native", ProviderID: "native"},
		},
	})

	assert.Eq(t, 2, len(resolved.ToolRefs))
	assert.Eq(t, 2, len(resolved.ToolNames))
	assert.Eq(t, "file_read", resolved.ToolNames[0])
	assert.Eq(t, "shell_exec", resolved.ToolNames[1])
}

func TestStaticToolProvider_ListToolRefsReturnsCopy(t *testing.T) {
	t.Parallel()

	provider := NewStaticToolProvider("native", []ToolRef{
		{Name: "file_read", Source: "native", ProviderID: "native"},
	})

	refs, err := provider.ListToolRefs(context.Background())
	assert.NoErr(t, err)
	assert.Eq(t, 1, len(refs))

	refs[0].Name = "changed"

	again, err := provider.ListToolRefs(context.Background())
	assert.NoErr(t, err)
	assert.Eq(t, "file_read", again[0].Name)
}
