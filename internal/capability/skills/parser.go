// Package skills loads SKILL.md files: YAML frontmatter (name, description,
// optional allowed-tools) followed by Markdown instructions.
package skills

import (
	"fmt"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
)

// Skill is one parsed SKILL.md.
type Skill struct {
	Name        string
	Description string
	// AllowedTools is informational only (shown, never enforced).
	AllowedTools []string
	// Path is the SKILL.md file; Dir its directory (other skill files live there).
	Path string
	Dir  string
}

type frontmatter struct {
	Name         string `yaml:"name"`
	Description  string `yaml:"description"`
	AllowedTools any    `yaml:"allowed-tools"`
}

// Parse splits data into the skill metadata and the Markdown body. The file
// must start with a "---" line and close the frontmatter with another; CRLF
// line endings are accepted.
func Parse(data []byte) (Skill, string, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimPrefix(text, "\xef\xbb\xbf")
	if !strings.HasPrefix(text, "---\n") {
		return Skill{}, "", fmt.Errorf("missing frontmatter: the file must start with ---")
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	var head, body string
	switch {
	case strings.HasPrefix(rest, "---"): // empty frontmatter
		head, body = "", strings.TrimPrefix(rest, "---")
	case end < 0:
		return Skill{}, "", fmt.Errorf("frontmatter is not closed with ---")
	default:
		head, body = rest[:end], rest[end+len("\n---"):]
	}
	// The closing line may carry trailing spaces; the body starts after it.
	if i := strings.IndexByte(body, '\n'); i >= 0 && strings.TrimSpace(body[:i]) == "" {
		body = body[i+1:]
	} else if strings.TrimSpace(body) == "" {
		body = ""
	}
	var fm frontmatter
	if err := yaml.Unmarshal([]byte(head), &fm); err != nil {
		return Skill{}, "", fmt.Errorf("parse frontmatter: %w", err)
	}
	s := Skill{
		Name:         strings.TrimSpace(fm.Name),
		Description:  strings.Join(strings.Fields(fm.Description), " "),
		AllowedTools: toolList(fm.AllowedTools),
	}
	if s.Name == "" {
		return Skill{}, "", fmt.Errorf("frontmatter has no name")
	}
	return s, strings.TrimLeft(body, "\n"), nil
}

// ParseFile reads and parses one SKILL.md.
func ParseFile(path string) (Skill, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, "", err
	}
	s, body, err := Parse(data)
	if err != nil {
		return Skill{}, "", fmt.Errorf("%s: %w", path, err)
	}
	return s, body, nil
}

// toolList accepts allowed-tools as a list or a comma/space separated string.
func toolList(v any) []string {
	var out []string
	switch t := v.(type) {
	case string:
		out = strings.FieldsFunc(t, func(r rune) bool { return r == ',' || r == ' ' })
	case []any:
		for _, item := range t {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	}
	return out
}
