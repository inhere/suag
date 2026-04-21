package skills

import (
	"strings"

	platformskills "github.com/inhere/axon/internal/platform/tools/skills"
)

// LoadDescriptorsFromFile reuses the existing SKILL.md parser but converts the
// result into capability descriptors instead of executable skill-tools.
func LoadDescriptorsFromFile(path string) ([]Descriptor, error) {
	parsed, err := platformskills.ParseSkillsFile(path)
	if err != nil {
		return nil, err
	}

	descriptors := make([]Descriptor, 0, len(parsed))
	for _, skill := range parsed {
		desc := Descriptor{
			Name:              strings.TrimSpace(skill.Name),
			Description:       strings.TrimSpace(skill.Description),
			SourcePath:        strings.TrimSpace(path),
			TriggerHints:      inferTriggerHints(skill),
			SystemPromptParts: inferPromptParts(skill),
			ToolAllowlist:     inferToolAllowlist(skill),
		}
		if desc.Name == "" {
			continue
		}
		descriptors = append(descriptors, desc)
	}
	return descriptors, nil
}

func inferTriggerHints(skill platformskills.Skill) []string {
	hints := []string{skill.Name}
	if strings.TrimSpace(skill.Description) != "" {
		hints = append(hints, skill.Description)
	}
	return cloneStrings(hints)
}

func inferPromptParts(skill platformskills.Skill) []string {
	if strings.TrimSpace(skill.Description) == "" {
		return nil
	}
	return []string{
		"[Skill " + strings.TrimSpace(skill.Name) + "] " + strings.TrimSpace(skill.Description),
	}
}

func inferToolAllowlist(skill platformskills.Skill) []string {
	desc := strings.ToLower(strings.TrimSpace(skill.Description))
	if desc == "" {
		return nil
	}

	var names []string
	if strings.Contains(desc, "read") || strings.Contains(desc, "inspect") || strings.Contains(desc, "review") ||
		strings.Contains(desc, "brainstorm") || strings.Contains(desc, "ask questions") ||
		strings.Contains(desc, "查看") || strings.Contains(desc, "检查") || strings.Contains(desc, "分析") {
		names = append(names, "file_read", "file_list")
	}
	if strings.Contains(desc, "write") || strings.Contains(desc, "edit") || strings.Contains(desc, "modify") ||
		strings.Contains(desc, "修复") || strings.Contains(desc, "修改") || strings.Contains(desc, "更新") {
		names = append(names, "file_write")
	}
	if strings.Contains(desc, "shell") || strings.Contains(desc, "command") || strings.Contains(desc, "test") ||
		strings.Contains(desc, "运行") || strings.Contains(desc, "命令") {
		names = append(names, "shell_exec")
	}
	return cloneStrings(names)
}
