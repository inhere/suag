package skills

import "strings"

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

	result := ResolvedSkillSet{}
	for _, desc := range descriptors {
		if len(result.Skills) >= maxSkills {
			break
		}
		if !matchesDescriptor(desc, message, recallJoined) {
			continue
		}
		result.Skills = append(result.Skills, cloneDescriptor(desc))
		result.SystemPromptParts = mergeUniqueStrings(result.SystemPromptParts, desc.SystemPromptParts)
		result.ToolAllowlist = mergeUniqueStrings(result.ToolAllowlist, desc.ToolAllowlist)
		result.ResourceHints = mergeUniqueResourceHints(result.ResourceHints, desc.ResourceHints)
	}
	return result
}

func matchesDescriptor(desc Descriptor, message, recallJoined string) bool {
	if strings.TrimSpace(desc.Name) == "" {
		return false
	}

	if containsText(message, desc.Name) || containsText(recallJoined, desc.Name) {
		return true
	}
	for _, hint := range desc.TriggerHints {
		if containsText(message, hint) || containsText(recallJoined, hint) {
			return true
		}
	}
	return false
}

func containsText(target, needle string) bool {
	target = strings.TrimSpace(strings.ToLower(target))
	needle = strings.TrimSpace(strings.ToLower(needle))
	if target == "" || needle == "" {
		return false
	}
	return strings.Contains(target, needle)
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
