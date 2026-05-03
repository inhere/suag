package skills

import (
	"sort"
	"strings"
)

// Resolver selects a small number of skills for the current turn. v0 keeps the
// matching deliberately lightweight so capability governance can evolve without
// committing to a heavy skills engine too early.
type Resolver struct {
	registry *Registry
}

func NewResolver(registry *Registry) *Resolver {
	return &Resolver{registry: registry}
}

func (r *Resolver) Resolve(input MatchInput) ResolvedSkillSet {
	if r == nil || r.registry == nil {
		return ResolvedSkillSet{}
	}

	descriptors := r.registry.List()
	if len(descriptors) == 0 {
		return ResolvedSkillSet{}
	}

	maxSkills := input.MaxSkills
	if maxSkills <= 0 {
		maxSkills = 2
	}

	message := strings.ToLower(strings.TrimSpace(input.UserMessage))
	recallJoined := strings.ToLower(strings.Join(input.RecallPreview, "\n"))
	turnScopes := detectTurnScopes(message, recallJoined)
	preferredSkillNames := normalizeStringSet(input.PreferredSkillNames)

	type rankedMatch struct {
		desc  Descriptor
		match SkillMatch
	}
	var ranked []rankedMatch
	result := ResolvedSkillSet{}
	for _, desc := range descriptors {
		match, ok := matchDescriptor(desc, message, recallJoined, turnScopes, preferredSkillNames)
		if !ok {
			continue
		}
		ranked = append(ranked, rankedMatch{
			desc:  cloneDescriptor(desc),
			match: match,
		})
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].match.Score > ranked[j].match.Score
	})

	for _, item := range ranked {
		if len(result.Skills) >= maxSkills {
			break
		}
		result.Skills = append(result.Skills, item.desc)
		result.Matches = append(result.Matches, item.match)
		result.SystemPromptParts = mergeUniqueStrings(result.SystemPromptParts, item.desc.SystemPromptParts)
		result.ToolAllowlist = mergeUniqueStrings(result.ToolAllowlist, item.desc.ToolAllowlist)
		result.ResourceHints = mergeUniqueResourceHints(result.ResourceHints, item.desc.ResourceHints)
	}
	return result
}

func matchDescriptor(desc Descriptor, message, recallJoined string, turnScopes []string, preferredSkillNames map[string]bool) (SkillMatch, bool) {
	if strings.TrimSpace(desc.Name) == "" {
		return SkillMatch{}, false
	}

	match := SkillMatch{
		SkillName: strings.TrimSpace(desc.Name),
	}
	normalizedName := strings.ToLower(strings.TrimSpace(desc.Name))
	if preferredSkillNames[normalizedName] {
		match.Score += 4
		match.MatchSources = mergeUniqueStrings(match.MatchSources, []string{"previous_plan"})
		match.MatchTerms = mergeUniqueStrings(match.MatchTerms, []string{desc.Name})
	}
	if containsText(message, desc.Name) {
		match.Score += 3
		match.MatchSources = mergeUniqueStrings(match.MatchSources, []string{"message"})
		match.MatchTerms = mergeUniqueStrings(match.MatchTerms, []string{desc.Name})
	}
	if containsText(recallJoined, desc.Name) {
		match.Score += 3
		match.MatchSources = mergeUniqueStrings(match.MatchSources, []string{"recall"})
		match.MatchTerms = mergeUniqueStrings(match.MatchTerms, []string{desc.Name})
	}
	for _, hint := range desc.TriggerHints {
		if containsText(message, hint) {
			match.Score += 2
			match.MatchSources = mergeUniqueStrings(match.MatchSources, []string{"message"})
			match.MatchTerms = mergeUniqueStrings(match.MatchTerms, []string{hint})
		}
		if containsText(recallJoined, hint) {
			match.Score += 1
			match.MatchSources = mergeUniqueStrings(match.MatchSources, []string{"recall"})
			match.MatchTerms = mergeUniqueStrings(match.MatchTerms, []string{hint})
		}
	}
	matchedScopes := intersectStrings(descriptorScopeHints(desc), turnScopes)
	if len(matchedScopes) > 0 {
		match.Score += 2 * len(matchedScopes)
		match.MatchSources = mergeUniqueStrings(match.MatchSources, []string{"scope"})
		match.MatchTerms = mergeUniqueStrings(match.MatchTerms, matchedScopes)
		match.ScopeHints = mergeUniqueStrings(match.ScopeHints, matchedScopes)
	}
	if match.Score == 0 {
		return SkillMatch{}, false
	}
	return match, true
}

func normalizeStringSet(items []string) map[string]bool {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]bool, len(items))
	for _, item := range items {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		out[item] = true
	}
	return out
}

func detectTurnScopes(message, recallJoined string) []string {
	var scopes []string
	if hasAnyText(message, []string{"brainstorm", "idea", "plan", "planning", "头脑风暴", "规划"}) ||
		hasAnyText(recallJoined, []string{"brainstorm", "idea", "plan", "planning", "头脑风暴", "规划"}) {
		scopes = append(scopes, "planning")
	}
	if hasAnyText(message, []string{"review", "inspect", "analy", "检查", "分析", "查看"}) ||
		hasAnyText(recallJoined, []string{"review", "inspect", "analy", "检查", "分析", "查看"}) {
		scopes = append(scopes, "analysis")
	}
	if hasAnyText(message, []string{"edit", "write", "modify", "修复", "修改", "更新"}) ||
		hasAnyText(recallJoined, []string{"edit", "write", "modify", "修复", "修改", "更新"}) {
		scopes = append(scopes, "edit")
	}
	if hasAnyText(message, []string{"run", "test", "command", "运行", "命令"}) ||
		hasAnyText(recallJoined, []string{"run", "test", "command", "运行", "命令"}) {
		scopes = append(scopes, "execution")
	}
	return mergeUniqueStrings(nil, scopes)
}

func containsText(target, needle string) bool {
	target = strings.TrimSpace(strings.ToLower(target))
	needle = strings.TrimSpace(strings.ToLower(needle))
	if target == "" || needle == "" {
		return false
	}
	return strings.Contains(target, needle)
}

func hasAnyText(target string, needles []string) bool {
	for _, needle := range needles {
		if containsText(target, needle) {
			return true
		}
	}
	return false
}

func descriptorScopeHints(desc Descriptor) []string {
	if scopes := mergeUniqueStrings(nil, desc.ScopeHints); len(scopes) > 0 {
		return scopes
	}
	return inferScopeHints(SkillSource{
		Name:        desc.Name,
		Description: desc.Description,
	})
}

func mergeUniqueStrings(base, extra []string) []string {
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

func mergeUniqueResourceHints(base, extra []ResourceHint) []ResourceHint {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(base)+len(extra))
	out := make([]ResourceHint, 0, len(base)+len(extra))
	for _, group := range [][]ResourceHint{base, extra} {
		for _, hint := range group {
			key := strings.TrimSpace(hint.SkillName) + "|" +
				strings.TrimSpace(hint.Kind) + "|" +
				strings.TrimSpace(hint.Path) + "|" +
				strings.TrimSpace(hint.SourcePath)
			if key == "|||" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, ResourceHint{
				SkillName:  strings.TrimSpace(hint.SkillName),
				Kind:       strings.TrimSpace(hint.Kind),
				Path:       strings.TrimSpace(hint.Path),
				SourcePath: strings.TrimSpace(hint.SourcePath),
				Priority:   hint.Priority,
			})
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func intersectStrings(left, right []string) []string {
	if len(left) == 0 || len(right) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(left))
	for _, item := range left {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		seen[item] = true
	}

	out := make([]string, 0, len(right))
	for _, item := range right {
		item = strings.TrimSpace(item)
		if item == "" || !seen[item] {
			continue
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return mergeUniqueStrings(nil, out)
}
