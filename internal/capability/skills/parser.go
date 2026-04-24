package skills

import (
	"fmt"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
)

// SkillSource is the lightweight parsed representation of a SKILL.md block used
// by capability-skill loading. It stays intentionally focused on metadata that
// v0 runtime governance actually consumes.
type SkillSource struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	Type        string            `yaml:"type"`
	Command     string            `yaml:"command"`
	InputSchema map[string]string `yaml:"inputs"`
}

// ParseSkillsFile reads a SKILL.md file and returns all defined skill blocks.
func ParseSkillsFile(path string) ([]SkillSource, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read skills file %q: %w", path, err)
	}
	return ParseSkillsBytes(data)
}

// ParseSkillsBytes parses one or more YAML skill blocks separated by "---".
func ParseSkillsBytes(data []byte) ([]SkillSource, error) {
	raw := string(data)
	blocks := strings.Split(raw, "\n---\n")

	var skills []SkillSource
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		block = strings.TrimPrefix(block, "---\n")
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}

		var skill SkillSource
		if err := yaml.Unmarshal([]byte(block), &skill); err != nil {
			return nil, fmt.Errorf("parse skill block: %w", err)
		}
		if strings.TrimSpace(skill.Name) == "" {
			continue
		}
		skills = append(skills, skill)
	}
	return skills, nil
}
