package agent

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Soul holds the parsed role definition from a SOUL.md file.
// SOUL.md uses YAML front matter (between --- markers) followed by an optional
// freeform persona description in the body.
type Soul struct {
	Name        string   `yaml:"name"`
	Role        string   `yaml:"role"`
	Expertise   []string `yaml:"expertise"`
	Skills      []string `yaml:"skills"`
	Tools       []string `yaml:"tools"`       // tool whitelist
	Constraints []string `yaml:"constraints"` // behavioural constraints
	Persona     string   `yaml:"persona"`     // full system prompt; if empty, built from fields above
}

// ParseSoul reads a SOUL.md file and returns the parsed Soul.
// The file must start with a YAML block delimited by "---" lines.
func ParseSoul(path string) (*Soul, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read soul file %q: %w", path, err)
	}
	return ParseSoulBytes(data)
}

// ParseSoulBytes parses a SOUL.md from raw bytes.
func ParseSoulBytes(data []byte) (*Soul, error) {
	// Extract YAML front matter between first pair of "---" lines.
	raw := string(data)
	yamlContent, err := extractFrontMatter(raw)
	if err != nil {
		// Fall back: treat entire file as YAML.
		yamlContent = raw
	}

	var soul Soul
	if err := yaml.Unmarshal([]byte(yamlContent), &soul); err != nil {
		return nil, fmt.Errorf("parse soul yaml: %w", err)
	}

	if soul.Persona == "" {
		soul.Persona = soul.buildPersona()
	}

	return &soul, nil
}

// extractFrontMatter returns the YAML content between the first two "---" delimiters.
func extractFrontMatter(content string) (string, error) {
	lines := splitLines(content)
	if len(lines) < 2 || lines[0] != "---" {
		return "", fmt.Errorf("no front matter found")
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return "", fmt.Errorf("front matter not closed")
	}

	var sb strings.Builder
	for _, l := range lines[1:end] {
		sb.WriteString(l)
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

// buildPersona constructs a system prompt from structured Soul fields.
func (s *Soul) buildPersona() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "You are %s, %s.\n\n", s.Name, s.Role)

	if len(s.Expertise) > 0 {
		sb.WriteString("Your areas of expertise include:\n")
		for _, e := range s.Expertise {
			sb.WriteString("- ")
			sb.WriteString(e)
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	if len(s.Constraints) > 0 {
		sb.WriteString("You must follow these constraints:\n")
		for _, c := range s.Constraints {
			sb.WriteString("- ")
			sb.WriteString(c)
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
	}

	return sb.String()
}

// splitLines splits a string into lines.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
