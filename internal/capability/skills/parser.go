// Package skills loads SKILL.md files: YAML frontmatter (name, description,
// optional allowed-tools) followed by Markdown instructions.
package skills

import (
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/goccy/go-yaml"
)

// Skill is one parsed SKILL.md.
type Skill struct {
	Name        string
	Description string
	// AllowedTools is informational only (shown, never enforced).
	AllowedTools []string
	// AlwaysLoad (frontmatter metadata.always_load or always_load) asks for
	// the body to be put in the system prompt instead of loaded on demand.
	AlwaysLoad bool
	// Workspace marks a skill found under the workspace (vs the user's home).
	Workspace bool
	// Path is the SKILL.md file; Dir its directory (other skill files live there).
	Path string
	Dir  string
}

type frontmatter struct {
	Name         string `yaml:"name"`
	Description  string `yaml:"description"`
	AllowedTools any    `yaml:"allowed-tools"`
	AlwaysLoad   bool   `yaml:"always_load"`
	Metadata     struct {
		AlwaysLoad bool `yaml:"always_load"`
	} `yaml:"metadata"`
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
		// Skill files are often written for tools that read frontmatter
		// leniently, e.g. an unquoted description containing ": ". Fall back
		// to a line-based reading before giving up.
		var ok bool
		if fm, ok = lenientFrontmatter(head); !ok {
			return Skill{}, "", fmt.Errorf("parse frontmatter: %s", firstLine(err.Error()))
		}
	}
	s := Skill{
		Name:         strings.TrimSpace(fm.Name),
		Description:  strings.Join(strings.Fields(fm.Description), " "),
		AllowedTools: toolList(fm.AllowedTools),
		AlwaysLoad:   fm.AlwaysLoad || fm.Metadata.AlwaysLoad,
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

// lenientFrontmatter reads "key: value" lines, splitting at the first ": "
// only, so values may contain colons. Indented lines continue the previous
// value (folded with a space), except under "metadata:" where they are its
// keys. Quotes around a value are dropped. ok is false when no name is found.
func lenientFrontmatter(head string) (frontmatter, bool) {
	var fm frontmatter
	values := map[string]string{}
	var last string // key whose value indented lines continue
	inMetadata := false
	for _, line := range strings.Split(head, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		indented := line[0] == ' ' || line[0] == '\t'
		key, value, hasColon := strings.Cut(strings.TrimSpace(line), ":")
		value = strings.TrimSpace(value)
		switch {
		case indented && inMetadata && hasColon:
			if strings.TrimSpace(key) == "always_load" {
				fm.Metadata.AlwaysLoad = value == "true"
			}
		case indented && last != "":
			values[last] = strings.TrimSpace(values[last] + " " + strings.TrimSpace(line))
		case !indented && hasColon:
			key = strings.TrimSpace(key)
			inMetadata, last = key == "metadata", key
			if value == ">" || value == "|" || value == ">-" || value == "|-" {
				value = ""
			}
			values[key] = strings.Trim(value, `"'`)
		}
	}
	fm.Name = values["name"]
	fm.Description = values["description"]
	fm.AlwaysLoad = values["always_load"] == "true"
	if tools := values["allowed-tools"]; tools != "" {
		fm.AllowedTools = strings.Trim(tools, "[]")
	}
	return fm, validLenientName(fm.Name)
}

// validLenientName accepts a skill name made of letters, digits, ".", "_"
// and "-", so a broken line like "name: [" is not taken as a name.
func validLenientName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

// firstLine keeps an error message to one line for the startup warning.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
