package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/testutil/assert"
)

func TestRegistry_ListReturnsCopies(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(Descriptor{
		Name:              "office-hours",
		Description:       "builder discovery skill",
		TriggerHints:      []string{"brainstorm this"},
		SystemPromptParts: []string{"Ask one question at a time."},
	})

	listed := registry.List()
	assert.Eq(t, 1, len(listed))
	listed[0].Name = "changed"

	again := registry.List()
	assert.Eq(t, "office-hours", again[0].Name)
}

func TestResolver_ResolveMatchesMessageAndRecall(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(Descriptor{
		Name:              "office-hours",
		Description:       "builder brainstorming skill",
		TriggerHints:      []string{"brainstorm", "idea"},
		SystemPromptParts: []string{"Ask one question at a time before implementation."},
		ToolAllowlist:     []string{"file_read", "file_list"},
	})
	registry.Register(Descriptor{
		Name:              "review",
		Description:       "review code diffs",
		TriggerHints:      []string{"review this"},
		SystemPromptParts: []string{"Prioritize risks and regressions."},
		ToolAllowlist:     []string{"file_read"},
	})

	resolver := NewResolver(registry)
	resolved := resolver.Resolve(MatchInput{
		UserMessage:   "我们先 brainstorm 一下这个产品想法",
		RecallPreview: []string{"之前提到 office-hours 更适合在实现前使用"},
		MaxSkills:     2,
	})

	assert.Eq(t, 1, len(resolved.Skills))
	assert.Eq(t, "office-hours", resolved.Skills[0].Name)
	assert.Eq(t, []string{"Ask one question at a time before implementation."}, resolved.SystemPromptParts)
	assert.Eq(t, []string{"file_read", "file_list"}, resolved.ToolAllowlist)
}

func TestResolver_ResolveHonorsMaxSkills(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(Descriptor{Name: "one", TriggerHints: []string{"task"}})
	registry.Register(Descriptor{Name: "two", TriggerHints: []string{"task"}})
	registry.Register(Descriptor{Name: "three", TriggerHints: []string{"task"}})

	resolver := NewResolver(registry)
	resolved := resolver.Resolve(MatchInput{
		UserMessage: "task",
		MaxSkills:   2,
	})

	assert.Eq(t, 2, len(resolved.Skills))
	assert.Eq(t, "one", resolved.Skills[0].Name)
	assert.Eq(t, "two", resolved.Skills[1].Name)
}

func TestLoadDescriptorsFromFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	err := os.MkdirAll(filepath.Join(dir, "references"), 0o755)
	assert.NoErr(t, err)
	err = os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
	assert.NoErr(t, err)
	err = os.WriteFile(filepath.Join(dir, "references", "guide.md"), []byte("# Guide\n\nAlways ask clarifying questions first.\n"), 0o644)
	assert.NoErr(t, err)
	err = os.WriteFile(filepath.Join(dir, "assets", "notes.txt"), []byte("Prefer compact summaries.\n"), 0o644)
	assert.NoErr(t, err)
	content := []byte(`---
name: office-hours
description: Ask questions before implementation and help brainstorm product ideas.
type: shell
command: echo hi
inputs:
  topic: Topic to discuss
---`)
	err = os.WriteFile(path, content, 0o644)
	assert.NoErr(t, err)

	descriptors, err := LoadDescriptorsFromFile(path)
	assert.NoErr(t, err)
	assert.Eq(t, 1, len(descriptors))
	assert.Eq(t, "office-hours", descriptors[0].Name)
	assert.True(t, len(descriptors[0].TriggerHints) >= 1)
	assert.True(t, len(descriptors[0].SystemPromptParts) >= 1)
	assert.Eq(t, []string{"file_read", "file_list"}, descriptors[0].ToolAllowlist)
	assert.Eq(t, 2, len(descriptors[0].ResourceHints))
	assert.True(t, filepath.IsAbs(descriptors[0].ResourceHints[0]))
	assert.True(t, filepath.IsAbs(descriptors[0].ResourceHints[1]))
}
