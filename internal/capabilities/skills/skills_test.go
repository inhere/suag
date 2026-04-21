package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gookit/goutil/testutil/assert"
)

func TestParseSkillsBytes(t *testing.T) {
	t.Parallel()

	parsed, err := ParseSkillsBytes([]byte(`---
name: office-hours
description: Ask questions before implementation.
type: shell
command: echo hi
inputs:
  topic: Topic to discuss
---
name: review
description: Review code changes for regressions.
type: shell
command: echo review
inputs:
  path: Path to review
`))
	assert.NoErr(t, err)
	assert.Eq(t, 2, len(parsed))
	assert.Eq(t, "office-hours", parsed[0].Name)
	assert.Eq(t, "review", parsed[1].Name)
	assert.Eq(t, "Topic to discuss", parsed[0].InputSchema["topic"])
}

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
	assert.Eq(t, 1, len(resolved.Matches))
	assert.Eq(t, "office-hours", resolved.Matches[0].SkillName)
	assert.True(t, resolved.Matches[0].Score > 0)
	assert.True(t, containsString(resolved.Matches[0].MatchSources, "message"))
	assert.True(t, containsString(resolved.Matches[0].MatchSources, "recall"))
	assert.True(t, containsString(resolved.Matches[0].MatchTerms, "brainstorm"))
	assert.Eq(t, []string{"Ask one question at a time before implementation."}, resolved.SystemPromptParts)
	assert.Eq(t, []string{"file_read", "file_list"}, resolved.ToolAllowlist)
}

func TestResolver_ResolveRanksStrongerMatchesFirst(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	registry.Register(Descriptor{
		Name:         "office-hours",
		Description:  "builder brainstorming skill",
		TriggerHints: []string{"brainstorm", "idea"},
	})
	registry.Register(Descriptor{
		Name:         "review",
		Description:  "review code diffs",
		TriggerHints: []string{"inspect"},
	})

	resolver := NewResolver(registry)
	resolved := resolver.Resolve(MatchInput{
		UserMessage:   "office-hours brainstorm this idea",
		RecallPreview: []string{"office-hours works well before implementation"},
		MaxSkills:     2,
	})

	assert.Eq(t, 1, len(resolved.Skills))
	assert.Eq(t, "office-hours", resolved.Skills[0].Name)
	assert.Eq(t, 1, len(resolved.Matches))
	assert.Eq(t, "office-hours", resolved.Matches[0].SkillName)
	assert.True(t, resolved.Matches[0].Score >= 6)
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
	assert.Eq(t, "office-hours", descriptors[0].ResourceHints[0].SkillName)
	assert.Eq(t, "reference", descriptors[0].ResourceHints[0].Kind)
	assert.True(t, descriptors[0].ResourceHints[0].Priority > descriptors[0].ResourceHints[1].Priority)
	assert.True(t, filepath.IsAbs(descriptors[0].ResourceHints[0].Path))
	assert.True(t, filepath.IsAbs(descriptors[0].ResourceHints[1].Path))
	assert.True(t, strings.HasSuffix(descriptors[0].ResourceHints[0].Path, filepath.Join("references", "guide.md")))
	assert.True(t, strings.HasSuffix(descriptors[0].ResourceHints[1].Path, filepath.Join("assets", "notes.txt")))
	assert.Eq(t, path, descriptors[0].ResourceHints[0].SourcePath)
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) == strings.TrimSpace(target) {
			return true
		}
	}
	return false
}
