package native

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/inhere/suag/internal/capability/skills"
	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/pkg/agentcore/api"
)

// maxSkillListFiles bounds the file list shown with a skill.
const maxSkillListFiles = 50

// SkillTool returns a skill's instructions (SKILL.md body) or another file of
// the skill's directory. Skill directories may lie outside the workspace, so
// reads go through os.OpenRoot(skillDir): nothing outside that directory.
type SkillTool struct {
	reg    *skills.Registry
	maxOut int // result cap in bytes; 0 = DefaultMaxOutputBytes
}

// NewSkillTool serves the skills in reg.
func NewSkillTool(reg *skills.Registry, maxOut int) *SkillTool {
	return &SkillTool{reg: reg, maxOut: maxOut}
}

func (t *SkillTool) Name() string     { return "skill" }
func (t *SkillTool) Kind() tools.Kind { return tools.KindNative }
func (t *SkillTool) Description() string {
	return "Load a skill listed in the system prompt: returns its instructions and the files in its directory. " +
		"Pass file to read one of those files (path relative to the skill directory). Follow the instructions you load."
}
func (t *SkillTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "Skill name"},
				"file": map[string]any{"type": "string", "description": "Optional file inside the skill directory, e.g. references/usage.md"},
			},
			"required": []any{"name"},
		},
	}
}

func (t *SkillTool) Execute(_ context.Context, call api.ToolCall) (*api.ToolResult, error) {
	name, _ := call.Arguments["name"].(string)
	s, ok := t.reg.Get(name)
	if !ok {
		var names []string
		for _, s := range t.reg.List() {
			names = append(names, s.Name)
		}
		return nil, fmt.Errorf("skill %q not found; available: %s", name, strings.Join(names, ", "))
	}
	root, err := os.OpenRoot(s.Dir)
	if err != nil {
		return nil, fmt.Errorf("skill %s: %w", s.Name, err)
	}
	defer root.Close()

	var content string
	if file, _ := call.Arguments["file"].(string); strings.TrimSpace(file) != "" {
		content, err = readSkillFile(root, filepath.Clean(filepath.FromSlash(file)))
		if err != nil {
			return nil, fmt.Errorf("skill %s file %s: %w", s.Name, file, err)
		}
	} else {
		_, body, err := skills.ParseFile(s.Path)
		if err != nil {
			return nil, err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "# Skill: %s\nDirectory: %s\n", s.Name, s.Dir)
		if len(s.AllowedTools) > 0 {
			fmt.Fprintf(&b, "Allowed tools (informational): %s\n", strings.Join(s.AllowedTools, ", "))
		}
		if files := skillFiles(root); len(files) > 0 {
			fmt.Fprintf(&b, "Files (read with file=...): %s\n", strings.Join(files, ", "))
		}
		b.WriteString("\n")
		b.WriteString(body)
		content = b.String()
	}
	return &api.ToolResult{CallID: call.CallID, Name: t.Name(), Content: TruncateHeadTail(content, t.maxOut)}, nil
}

// readSkillFile reads rel through root, which refuses paths and symlinks
// leaving the skill directory.
func readSkillFile(root *os.Root, rel string) (string, error) {
	f, err := root.Open(rel)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, binarySniffBytes*64))
	if err != nil {
		return "", err
	}
	if isBinaryHead(data[:min(len(data), binarySniffBytes)]) {
		return fmt.Sprintf("[binary file, %d bytes; not shown]", len(data)), nil
	}
	return string(data), nil
}

// skillFiles lists the files of the skill directory except SKILL.md (two
// levels, slash-separated).
func skillFiles(root *os.Root) []string {
	var out []string
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		d, err := root.Open(dir)
		if err != nil {
			return
		}
		entries, _ := d.ReadDir(-1)
		d.Close()
		for _, e := range entries {
			rel := filepath.ToSlash(filepath.Join(dir, e.Name()))
			switch {
			case e.IsDir() && depth < 2 && !strings.HasPrefix(e.Name(), "."):
				walk(filepath.Join(dir, e.Name()), depth+1)
			case e.Type().IsRegular() && !strings.EqualFold(rel, "SKILL.md"):
				out = append(out, rel)
			}
		}
	}
	walk(".", 1)
	sort.Strings(out)
	if len(out) > maxSkillListFiles {
		out = append(out[:maxSkillListFiles], "…")
	}
	return out
}
