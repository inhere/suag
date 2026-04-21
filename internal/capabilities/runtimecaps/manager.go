package runtimecaps

import "context"

// CapabilityManager builds session capability sets from registered providers.
type CapabilityManager struct {
	toolProviders []ToolProvider
}

func NewManager() *CapabilityManager {
	return &CapabilityManager{}
}

func (m *CapabilityManager) MustRegisterToolProvider(provider ToolProvider) {
	m.toolProviders = append(m.toolProviders, provider)
}

func (m *CapabilityManager) BuildDefaultCapabilitySet(input BuildSetInput) CapabilitySet {
	if set, ok := m.DecodeCapabilitySetMetadata(input.SessionMetadata); ok {
		return set
	}

	set := CapabilitySet{}
	for _, provider := range m.toolProviders {
		refs, err := provider.ListToolRefs(context.Background())
		if err != nil {
			panic(err)
		}
		set.Tools = append(set.Tools, cloneToolRefs(refs)...)
	}
	return set
}

func (m *CapabilityManager) ResolveTurnCapabilities(set CapabilitySet) ResolvedTurnCapabilities {
	resolved := ResolvedTurnCapabilities{
		ToolRefs: cloneToolRefs(set.Tools),
	}
	for _, ref := range resolved.ToolRefs {
		resolved.ToolNames = append(resolved.ToolNames, ref.Name)
	}
	return resolved
}

func (m *CapabilityManager) ResolveTurnCapabilitiesFromInput(input ResolveTurnInput) ResolvedTurnCapabilities {
	set := m.BuildDefaultCapabilitySet(BuildSetInput{
		SessionMetadata: input.SessionMetadata,
	})
	return m.ResolveTurnCapabilities(set)
}

func (m *CapabilityManager) EncodeCapabilitySetMetadata(set CapabilitySet) map[string]any {
	if len(set.Tools) == 0 {
		return nil
	}

	return map[string]any{
		CapabilitySetSessionMetadataKey: map[string]any{
			"tools": encodeToolRefs(set.Tools),
		},
	}
}

func (m *CapabilityManager) DecodeCapabilitySetMetadata(metadata map[string]any) (CapabilitySet, bool) {
	if len(metadata) == 0 {
		return CapabilitySet{}, false
	}

	raw, ok := metadata[CapabilitySetSessionMetadataKey]
	if !ok {
		return CapabilitySet{}, false
	}

	root, ok := raw.(map[string]any)
	if !ok {
		return CapabilitySet{}, false
	}

	tools := decodeToolRefs(root["tools"])
	if len(tools) == 0 {
		return CapabilitySet{}, false
	}
	return CapabilitySet{Tools: tools}, true
}

func NewStaticToolProvider(id string, refs []ToolRef) ToolProvider {
	return &staticToolProvider{
		id:    id,
		tools: cloneToolRefs(refs),
	}
}

func (p *staticToolProvider) ID() string {
	return p.id
}

func (p *staticToolProvider) ListToolRefs(ctx context.Context) ([]ToolRef, error) {
	_ = ctx
	return cloneToolRefs(p.tools), nil
}

func cloneToolRefs(refs []ToolRef) []ToolRef {
	if len(refs) == 0 {
		return nil
	}

	cloned := make([]ToolRef, len(refs))
	copy(cloned, refs)
	return cloned
}

func encodeToolRefs(refs []ToolRef) []map[string]any {
	if len(refs) == 0 {
		return nil
	}

	out := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		out = append(out, map[string]any{
			"name":        ref.Name,
			"source":      ref.Source,
			"provider_id": ref.ProviderID,
		})
	}
	return out
}

func decodeToolRefs(raw any) []ToolRef {
	switch value := raw.(type) {
	case []map[string]any:
		refs := make([]ToolRef, 0, len(value))
		for _, item := range value {
			ref, ok := decodeToolRef(item)
			if !ok {
				continue
			}
			refs = append(refs, ref)
		}
		return refs
	case []any:
		refs := make([]ToolRef, 0, len(value))
		for _, item := range value {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			ref, ok := decodeToolRef(itemMap)
			if !ok {
				continue
			}
			refs = append(refs, ref)
		}
		return refs
	default:
		return nil
	}
}

func decodeToolRef(item map[string]any) (ToolRef, bool) {
	if len(item) == 0 {
		return ToolRef{}, false
	}

	name, _ := item["name"].(string)
	source, _ := item["source"].(string)
	providerID, _ := item["provider_id"].(string)
	if name == "" {
		return ToolRef{}, false
	}
	return ToolRef{
		Name:       name,
		Source:     source,
		ProviderID: providerID,
	}, true
}
