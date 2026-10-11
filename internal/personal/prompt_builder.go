package personal

import (
	"context"
	"strings"

	"github.com/inhere/suag/pkg/agentcore"
)

// promptMetaPatch returns the session metadata keys this turn changes and the
// short notice for the model about them. The patch is applied with
// PatchSessionMeta so concurrent writers cannot be rolled back.
func (r *Runner) promptMetaPatch(model Session) (map[string]any, string, error) {
	metaPatch := map[string]any{}
	var rulesUpdate, appendUpdate string
	if r.cfg.RuleSource != nil {
		rules, hash, err := r.cfg.RuleSource()
		if err != nil {
			return nil, "", err
		}
		if previous, _ := model.Meta["personal_rules_hash"].(string); previous != hash {
			metaPatch["personal_rules_hash"] = hash
			metaPatch["personal_rules_content"] = rules
			// The system prompt is built from the metadata updated above, so it
			// already carries the new rules this turn. Only send a short notice
			// instead of repeating the full text (up to 128KB, resent per step).
			rulesUpdate = "项目规则已更新，以 system prompt 中的版本为准"
		}
	}

	// Resume with a caller-supplied append prompt (--append-system-prompt or
	// SUAG_APPEND_SYSTEM_PROMPT): when it differs from the persisted one, replace
	// it and send a short notice (same mechanism as rules; the full text lives
	// only in the system prompt). An unset value keeps the persisted one; an
	// explicitly supplied empty value clears it.
	if appended := r.cfg.AppendSystemPrompt; appended != "" || r.cfg.AppendSystemPromptSet {
		if previous, _ := model.Meta["personal_append_prompt"].(string); previous != appended {
			// An empty string (not nil) so PatchSessionMeta stores it instead of deleting the key.
			metaPatch["personal_append_prompt"] = appended
			appendUpdate = "附加提示已更新，以 system prompt 中的版本为准"
			if appended == "" {
				appendUpdate = "附加提示已清除，不再追加"
			}
		}
	}
	return metaPatch, joinContext(rulesUpdate, appendUpdate), nil
}

type rulesPromptBuilder struct {
	base     agentcore.PromptBuilder
	metadata map[string]any
	// deferredTools is the stable deferred-tool listing ("" when none).
	deferredTools string
}

func (b rulesPromptBuilder) BuildSystemPrompt(ctx context.Context, state *agentcore.RunState) (string, error) {
	var parts []string
	if b.base != nil {
		base, err := b.base.BuildSystemPrompt(ctx, state)
		if err != nil {
			return "", err
		}
		parts = append(parts, base)
	}
	// The memory index snapshot goes right after the rules.
	for _, key := range []string{"personal_rules_content", metaMemoryIndexContent, "personal_append_prompt"} {
		if value, _ := b.metadata[key].(string); value != "" {
			parts = append(parts, value)
		}
	}
	if b.deferredTools != "" {
		parts = append(parts, b.deferredTools)
	}
	return strings.Join(parts, "\n\n"), nil
}
