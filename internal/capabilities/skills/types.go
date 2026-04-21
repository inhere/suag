package skills

// Descriptor is the runtime-facing capability recipe distilled from a SKILL.md
// style source. It is intentionally smaller than any executable skill runner.
type Descriptor struct {
	Name              string
	Description       string
	SourcePath        string
	TriggerHints      []string
	ToolAllowlist     []string
	SystemPromptParts []string
	ResourceHints     []string
}

// MatchInput carries the minimal turn signals used to decide whether a skill
// should influence the current run.
type MatchInput struct {
	UserMessage   string
	RecallPreview []string
	MaxSkills     int
}

// ResolvedSkillSet is the turn-time skill selection result.
type ResolvedSkillSet struct {
	Skills            []Descriptor
	SystemPromptParts []string
	ToolAllowlist     []string
	ResourceHints     []string
}
