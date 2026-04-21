package runtimecaps

import (
	"context"
	"strings"
)

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
		ToolMode: normalizeTurnToolMode(set.Policies.ToolMode),
	}
	allowed := normalizeNameSet(set.Policies.ToolAllowlist)
	denied := normalizeNameSet(set.Policies.ToolDenylist)
	refs := resolved.ToolRefs[:0]
	for _, ref := range resolved.ToolRefs {
		name := strings.TrimSpace(ref.Name)
		if name == "" {
			continue
		}
		if len(allowed) > 0 && !allowed[name] {
			continue
		}
		if denied[name] {
			continue
		}
		refs = append(refs, ref)
		resolved.ToolNames = append(resolved.ToolNames, name)
	}
	resolved.ToolRefs = refs
	return resolved
}

func (m *CapabilityManager) ResolveTurnCapabilitiesFromInput(input ResolveTurnInput) ResolvedTurnCapabilities {
	set := m.BuildDefaultCapabilitySet(BuildSetInput{
		SessionMetadata: input.SessionMetadata,
	})
	set.Policies = refineCapabilityPolicy(set.Policies, input)
	return m.ResolveTurnCapabilities(set)
}

func (m *CapabilityManager) EncodeCapabilitySetMetadata(set CapabilitySet) map[string]any {
	if len(set.Tools) == 0 {
		return nil
	}

	return map[string]any{
		CapabilitySetSessionMetadataKey: map[string]any{
			"tools":    encodeToolRefs(set.Tools),
			"policies": encodeCapabilityPolicy(set.Policies),
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
	policies := decodeCapabilityPolicy(root["policies"])
	if len(tools) == 0 {
		return CapabilitySet{}, false
	}
	return CapabilitySet{
		Tools:    tools,
		Policies: policies,
	}, true
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

func encodeCapabilityPolicy(policy CapabilityPolicy) map[string]any {
	mode := normalizeTurnToolMode(policy.ToolMode)
	allowlist := cloneStringSlice(policy.ToolAllowlist)
	denylist := cloneStringSlice(policy.ToolDenylist)
	if mode == "" && len(allowlist) == 0 && len(denylist) == 0 {
		return nil
	}

	out := map[string]any{}
	if mode != "" {
		out["tool_mode"] = string(mode)
	}
	if len(allowlist) > 0 {
		out["tool_allowlist"] = allowlist
	}
	if len(denylist) > 0 {
		out["tool_denylist"] = denylist
	}
	return out
}

func decodeCapabilityPolicy(raw any) CapabilityPolicy {
	root, ok := raw.(map[string]any)
	if !ok || len(root) == 0 {
		return CapabilityPolicy{}
	}

	mode, _ := root["tool_mode"].(string)
	return CapabilityPolicy{
		ToolMode:      normalizeTurnToolMode(TurnToolMode(mode)),
		ToolAllowlist: decodeStringSlice(root["tool_allowlist"]),
		ToolDenylist:  decodeStringSlice(root["tool_denylist"]),
	}
}

func refineCapabilityPolicy(policy CapabilityPolicy, input ResolveTurnInput) CapabilityPolicy {
	refined := CapabilityPolicy{
		ToolMode:      normalizeTurnToolMode(policy.ToolMode),
		ToolAllowlist: cloneStringSlice(policy.ToolAllowlist),
		ToolDenylist:  cloneStringSlice(policy.ToolDenylist),
	}

	if refined.ToolMode == "" {
		refined.ToolMode = TurnToolModeAuto
	}
	if refined.ToolMode != TurnToolModeAuto {
		return refined
	}

	message := strings.ToLower(strings.TrimSpace(input.UserMessage))
	if message == "" {
		if recallLooksReadOnly(input.RecallMetadata) {
			refined.ToolMode = TurnToolModeReadOnly
			refined.ToolDenylist = mergeStringSets(refined.ToolDenylist, []string{"file_write", "shell_exec"})
		}
		return refined
	}

	if turnLooksReadOnly(message) {
		refined.ToolMode = TurnToolModeReadOnly
		refined.ToolDenylist = mergeStringSets(refined.ToolDenylist, []string{"file_write", "shell_exec"})
		return refined
	}
	if recallLooksReadOnly(input.RecallMetadata) {
		refined.ToolMode = TurnToolModeReadOnly
		refined.ToolDenylist = mergeStringSets(refined.ToolDenylist, []string{"file_write", "shell_exec"})
	}
	return refined
}

func turnLooksReadOnly(message string) bool {
	readOnlyHints := []string{
		"read", "look at", "inspect", "analyze", "review", "check", "search",
		"查看", "看看", "检查", "分析", "审查", "搜索", "读取", "只读",
	}
	readOnlyWriteGuards := []string{
		"don't modify", "do not modify", "don't change", "do not change",
		"不要修改", "不用修改", "无需修改", "先不要修改", "别修改",
		"不要改", "先别改", "不要执行", "不要运行",
	}
	writeHints := []string{
		"write", "edit", "change", "modify", "update", "create", "fix", "run", "execute",
		"写", "改", "修改", "更新", "创建", "修复", "运行", "执行",
	}

	hasReadOnlyHint := false
	for _, hint := range readOnlyHints {
		if strings.Contains(message, hint) {
			hasReadOnlyHint = true
			break
		}
	}
	if !hasReadOnlyHint {
		return false
	}
	if containsAny(message, readOnlyWriteGuards) {
		return true
	}
	for _, hint := range writeHints {
		if strings.Contains(message, hint) {
			return false
		}
	}
	return true
}

func containsAny(message string, hints []string) bool {
	for _, hint := range hints {
		if strings.Contains(message, hint) {
			return true
		}
	}
	return false
}

func recallLooksReadOnly(metadata map[string]any) bool {
	if len(metadata) == 0 {
		return false
	}

	recall, ok := metadata["memory_recall"].(map[string]any)
	if !ok || len(recall) == 0 {
		return false
	}
	sources, ok := recall["sources"].([]map[string]any)
	if ok {
		return recallPreviewLooksReadOnly(sources)
	}
	rawSources, ok := recall["sources"].([]any)
	if !ok {
		return false
	}
	sources = make([]map[string]any, 0, len(rawSources))
	for _, item := range rawSources {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		sources = append(sources, itemMap)
	}
	return recallPreviewLooksReadOnly(sources)
}

func recallPreviewLooksReadOnly(sources []map[string]any) bool {
	for _, source := range sources {
		preview := decodeStringSlice(source["preview"])
		for _, line := range preview {
			line = strings.ToLower(strings.TrimSpace(line))
			if line == "" {
				continue
			}
			if containsAny(line, []string{
				"read-only", "read only", "inspect", "review", "analyze", "analysis",
				"只读", "查看", "检查", "分析", "审查",
			}) {
				return true
			}
		}
	}
	return false
}

func normalizeTurnToolMode(mode TurnToolMode) TurnToolMode {
	switch strings.ToLower(strings.TrimSpace(string(mode))) {
	case "", "auto":
		return TurnToolModeAuto
	case "full":
		return TurnToolModeFull
	case "read_only":
		return TurnToolModeReadOnly
	default:
		return TurnToolModeAuto
	}
}

func normalizeNameSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}

	out := make(map[string]bool, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out[name] = true
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func decodeStringSlice(raw any) []string {
	switch value := raw.(type) {
	case []string:
		return cloneStringSlice(value)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			text, ok := item.(string)
			if !ok {
				continue
			}
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			out = append(out, text)
		}
		return out
	default:
		return nil
	}
}

func cloneStringSlice(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	out := make([]string, 0, len(src))
	for _, item := range src {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeStringSets(base []string, extra []string) []string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	for _, group := range [][]string{base, extra} {
		for _, item := range group {
			item = strings.TrimSpace(item)
			if item == "" || seen[item] {
				continue
			}
			seen[item] = true
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
