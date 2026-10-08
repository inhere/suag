package runtimecaps

import (
	"context"
	"testing"

	"github.com/gookit/goutil/x/assert"
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

func TestCapabilityManager_BuildDefaultCapabilitySetPrefersSessionMetadata(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	manager.MustRegisterToolProvider(NewStaticToolProvider("native", []ToolRef{
		{Name: "file_read", Source: "native", ProviderID: "native"},
		{Name: "shell_exec", Source: "native", ProviderID: "native"},
	}))

	input := BuildSetInput{
		SessionMetadata: map[string]any{
			CapabilitySetSessionMetadataKey: map[string]any{
				"tools": []map[string]any{
					{"name": "file_read", "source": "native", "provider_id": "native"},
				},
			},
		},
	}

	set := manager.BuildDefaultCapabilitySet(input)

	assert.Eq(t, 1, len(set.Tools))
	assert.Eq(t, "file_read", set.Tools[0].Name)
}

func TestCapabilityManager_EncodeAndDecodeCapabilitySetMetadata(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	metadata := manager.EncodeCapabilitySetMetadata(CapabilitySet{
		Tools: []ToolRef{
			{Name: "file_read", Source: "native", ProviderID: "native"},
			{Name: "mcp_echo", Source: "mcp", ProviderID: "mcp.stub"},
		},
		Policies: CapabilityPolicy{
			ToolMode:      TurnToolModeReadOnly,
			ToolAllowlist: []string{"file_read", "mcp_echo"},
			ToolDenylist:  []string{"shell_exec"},
		},
	})

	set, ok := manager.DecodeCapabilitySetMetadata(metadata)
	assert.True(t, ok)
	assert.Eq(t, 2, len(set.Tools))
	assert.Eq(t, "file_read", set.Tools[0].Name)
	assert.Eq(t, "mcp_echo", set.Tools[1].Name)
	assert.Eq(t, TurnToolModeReadOnly, set.Policies.ToolMode)
	assert.Eq(t, []string{"file_read", "mcp_echo"}, set.Policies.ToolAllowlist)
	assert.Eq(t, []string{"shell_exec"}, set.Policies.ToolDenylist)
}

func TestCapabilityManager_ResolveTurnCapabilitiesUsesSessionMetadataBaseline(t *testing.T) {
	t.Parallel()

	manager := NewManager()

	resolved := manager.ResolveTurnCapabilitiesFromInput(ResolveTurnInput{
		SessionMetadata: map[string]any{
			CapabilitySetSessionMetadataKey: map[string]any{
				"tools": []map[string]any{
					{"name": "file_read", "source": "native", "provider_id": "native"},
					{"name": "mcp_echo", "source": "mcp", "provider_id": "mcp.stub"},
				},
			},
		},
	})

	assert.Eq(t, 2, len(resolved.ToolRefs))
	assert.Eq(t, 2, len(resolved.ToolNames))
	assert.Eq(t, "file_read", resolved.ToolNames[0])
	assert.Eq(t, "mcp_echo", resolved.ToolNames[1])
}

func TestCapabilityManager_ResolveTurnCapabilitiesAppliesPolicyFilters(t *testing.T) {
	t.Parallel()

	manager := NewManager()

	resolved := manager.ResolveTurnCapabilities(CapabilitySet{
		Tools: []ToolRef{
			{Name: "file_read", Source: "native", ProviderID: "native"},
			{Name: "shell_exec", Source: "native", ProviderID: "native"},
			{Name: "mcp_echo", Source: "mcp", ProviderID: "mcp.stub"},
		},
		Policies: CapabilityPolicy{
			ToolMode:      TurnToolModeReadOnly,
			ToolAllowlist: []string{"file_read", "mcp_echo"},
			ToolDenylist:  []string{"mcp_echo"},
		},
	})

	assert.Eq(t, TurnToolModeReadOnly, resolved.ToolMode)
	assert.Eq(t, 1, len(resolved.ToolRefs))
	assert.Eq(t, []string{"file_read"}, resolved.ToolNames)
}

func TestCapabilityManager_ResolveTurnCapabilitiesFromInputDerivesReadOnlyModeFromMessage(t *testing.T) {
	t.Parallel()

	manager := NewManager()

	resolved := manager.ResolveTurnCapabilitiesFromInput(ResolveTurnInput{
		UserMessage: "请先查看并分析当前 session 里的文件，不要修改",
		SessionMetadata: map[string]any{
			CapabilitySetSessionMetadataKey: map[string]any{
				"tools": []map[string]any{
					{"name": "file_read", "source": "native", "provider_id": "native"},
					{"name": "file_write", "source": "native", "provider_id": "native"},
					{"name": "shell_exec", "source": "native", "provider_id": "native"},
				},
				"policies": map[string]any{
					"tool_mode": "auto",
				},
			},
		},
	})

	assert.Eq(t, TurnToolModeReadOnly, resolved.ToolMode)
	assert.Eq(t, []string{"file_read"}, resolved.ToolNames)
}

func TestCapabilityManager_ResolveTurnCapabilitiesFromInputKeepsFullModeWhenPolicyExplicit(t *testing.T) {
	t.Parallel()

	manager := NewManager()

	resolved := manager.ResolveTurnCapabilitiesFromInput(ResolveTurnInput{
		UserMessage: "请先查看并分析当前 session 里的文件，不要修改",
		SessionMetadata: map[string]any{
			CapabilitySetSessionMetadataKey: map[string]any{
				"tools": []map[string]any{
					{"name": "file_read", "source": "native", "provider_id": "native"},
					{"name": "file_write", "source": "native", "provider_id": "native"},
				},
				"policies": map[string]any{
					"tool_mode": "full",
				},
			},
		},
	})

	assert.Eq(t, TurnToolModeFull, resolved.ToolMode)
	assert.Eq(t, []string{"file_read", "file_write"}, resolved.ToolNames)
}

func TestCapabilityManager_ResolveTurnCapabilitiesFromInputDerivesReadOnlyModeFromRecallMetadata(t *testing.T) {
	t.Parallel()

	manager := NewManager()

	resolved := manager.ResolveTurnCapabilitiesFromInput(ResolveTurnInput{
		UserMessage: "继续处理一下",
		SessionMetadata: map[string]any{
			CapabilitySetSessionMetadataKey: map[string]any{
				"tools": []map[string]any{
					{"name": "file_read", "source": "native", "provider_id": "native"},
					{"name": "file_write", "source": "native", "provider_id": "native"},
					{"name": "shell_exec", "source": "native", "provider_id": "native"},
				},
				"policies": map[string]any{
					"tool_mode": "auto",
				},
			},
		},
		RecallMetadata: map[string]any{
			"memory_recall": map[string]any{
				"sources": []map[string]any{
					{
						"name":        "MEMORY",
						"entry_count": 1,
						"preview":     []string{"- [turn_learning] 先查看相关文件并分析问题，不要直接修改"},
					},
				},
			},
		},
	})

	assert.Eq(t, TurnToolModeReadOnly, resolved.ToolMode)
	assert.Eq(t, []string{"file_read"}, resolved.ToolNames)
}

func TestCapabilityManager_ResolveTurnCapabilitiesFromInputUsesPreviousNextTurnPlan(t *testing.T) {
	t.Parallel()

	manager := NewManager()

	resolved := manager.ResolveTurnCapabilitiesFromInput(ResolveTurnInput{
		UserMessage: "继续处理一下",
		SessionMetadata: map[string]any{
			CapabilitySetSessionMetadataKey: map[string]any{
				"tools": []map[string]any{
					{"name": "file_read", "source": "native", "provider_id": "native"},
					{"name": "file_list", "source": "native", "provider_id": "native"},
					{"name": "file_write", "source": "native", "provider_id": "native"},
					{"name": "shell_exec", "source": "native", "provider_id": "native"},
				},
				"policies": map[string]any{
					"tool_mode": "auto",
				},
			},
		},
		PreviousNextTurnPlan: map[string]any{
			"tool_mode":      "read_only",
			"tool_allowlist": []string{"file_read", "file_list"},
		},
	})

	assert.Eq(t, TurnToolModeReadOnly, resolved.ToolMode)
	assert.Eq(t, []string{"file_read", "file_list"}, resolved.ToolNames)
}

func TestCapabilityManager_ResolveTurnCapabilitiesFromInputIgnoresPreviousReadOnlyPlanForWriteIntent(t *testing.T) {
	t.Parallel()

	manager := NewManager()

	resolved := manager.ResolveTurnCapabilitiesFromInput(ResolveTurnInput{
		UserMessage: "继续修改这个文件",
		SessionMetadata: map[string]any{
			CapabilitySetSessionMetadataKey: map[string]any{
				"tools": []map[string]any{
					{"name": "file_read", "source": "native", "provider_id": "native"},
					{"name": "file_write", "source": "native", "provider_id": "native"},
				},
				"policies": map[string]any{
					"tool_mode": "auto",
				},
			},
		},
		PreviousNextTurnPlan: map[string]any{
			"tool_mode":      "read_only",
			"tool_allowlist": []string{"file_read"},
		},
	})

	assert.Eq(t, TurnToolModeAuto, resolved.ToolMode)
	assert.Eq(t, []string{"file_read", "file_write"}, resolved.ToolNames)
}
