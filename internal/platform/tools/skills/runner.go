package skills

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/inhere/axon/internal/platform/llm"
	"github.com/inhere/axon/internal/platform/tools"
	"github.com/inhere/axon/pkg/agentcore/api"
)

const skillTimeout = 60 * time.Second

// SkillTool wraps a Skill definition as a tools.Tool.
type SkillTool struct {
	skill     Skill
	workspace string
}

// NewSkillTool creates a SkillTool for the given skill, scoped to workspace.
func NewSkillTool(skill Skill, workspace string) *SkillTool {
	return &SkillTool{skill: skill, workspace: workspace}
}

func (t *SkillTool) Name() string        { return t.skill.Name }
func (t *SkillTool) Description() string { return t.skill.Description }
func (t *SkillTool) Kind() tools.Kind    { return tools.KindSkill }
func (t *SkillTool) Schema() llm.ToolSchema {
	props := make(map[string]*llm.SchemaProperty)
	for name, desc := range t.skill.InputSchema {
		props[name] = &llm.SchemaProperty{Type: "string", Description: desc}
	}
	return llm.ToolSchema{
		Name:        t.skill.Name,
		Description: t.skill.Description,
		Parameters: llm.SchemaProperty{
			Type:       "object",
			Properties: props,
		},
	}
}

func (t *SkillTool) Execute(ctx context.Context, call api.ToolCall) (*api.ToolResult, error) {
	command := t.skill.Command

	// Replace {{workspace}} placeholder.
	command = strings.ReplaceAll(command, "{{workspace}}", t.workspace)

	// Replace {{args.KEY}} placeholders.
	for k, v := range call.Arguments {
		val, _ := v.(string)
		command = strings.ReplaceAll(command, "{{args."+k+"}}", val)
	}

	ctx, cancel := context.WithTimeout(ctx, skillTimeout)
	defer cancel()

	var cmd *exec.Cmd
	switch t.skill.Type {
	case "python":
		cmd = exec.CommandContext(ctx, "python3", "-c", command)
	default: // "shell"
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	cmd.Dir = t.workspace

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("skill %q timed out", t.skill.Name)
		}
		return nil, fmt.Errorf("skill %q failed: %w", t.skill.Name, err)
	}
	return &api.ToolResult{
		CallID:  call.CallID,
		Name:    t.skill.Name,
		Content: strings.TrimSpace(stdout.String()),
	}, nil
}

// LoadSkillsFromFile parses a SKILL.md file and returns ready-to-register SkillTools.
func LoadSkillsFromFile(path, workspace string) ([]*SkillTool, error) {
	skills, err := ParseSkillsFile(path)
	if err != nil {
		return nil, err
	}
	out := make([]*SkillTool, len(skills))
	for i, s := range skills {
		out[i] = NewSkillTool(s, workspace)
	}
	return out, nil
}
