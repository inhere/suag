// Package skills provides parsing and execution of SKILL.md-defined tools.
package skills

import (
	"fmt"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
)

// Skill represents a parsed SKILL.md entry.
type Skill struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	Type        string            `yaml:"type"`    // "shell" | "python"
	Command     string            `yaml:"command"` // command template (supports {{workspace}}, {{args.*}})
	InputSchema map[string]string `yaml:"inputs"`  // param name -> description
}

// ParseSkillsFile reads a SKILL.md file and returns all defined skills.
// Each skill block is separated by "---" delimiters.
func ParseSkillsFile(path string) ([]Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read skills file %q: %w", path, err)
	}
	return ParseSkillsBytes(data)
}

// ParseSkillsBytes parses one or more YAML skill blocks separated by "---".
func ParseSkillsBytes(data []byte) ([]Skill, error) {
	raw := string(data)
	blocks := strings.Split(raw, "\n---\n")

	var skills []Skill
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		// Strip leading "---" if present (first block).
		block = strings.TrimPrefix(block, "---\n")
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}

		var s Skill
		if err := yaml.Unmarshal([]byte(block), &s); err != nil {
			return nil, fmt.Errorf("parse skill block: %w", err)
		}
		if s.Name == "" {
			continue
		}
		skills = append(skills, s)
	}
	return skills, nil
}
