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
	_ = input

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
