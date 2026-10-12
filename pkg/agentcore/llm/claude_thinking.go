package llm

import (
	"strings"

	agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"
)

// Extended thinking for the Claude provider (ClaudeConfig.Thinking). Claude
// 4.6 and later models take {"thinking": {"type": "adaptive"}} with the depth
// set by output_config.effort; 4.7+ and the Claude 5 family reject the older
// {"type": "enabled", "budget_tokens": N}, which earlier models (4.5 and
// before) still need, with the interleaved-thinking beta header for thinking
// between tool calls.

// claudeThinkingMaxTokens is max_tokens when thinking is on and the request
// sets none: thinking counts toward max_tokens, so the 4096 default would cut
// replies short.
const claudeThinkingMaxTokens = 32000

// claudeInterleavedBeta enables thinking between tool calls for budget mode.
const claudeInterleavedBeta = "interleaved-thinking-2025-05-14"

// claudeBudgets maps a thinking level to budget_tokens (budget mode only).
var claudeBudgets = map[string]int{
	"minimal": 1024, "low": 2048, "medium": 8192, "high": 16384, "xhigh": 24576, "max": 31000,
}

// claudeEfforts maps a thinking level to output_config.effort; minimal has no
// counterpart and becomes low.
var claudeEfforts = map[string]string{
	"minimal": "low", "low": "low", "medium": "medium", "high": "high", "xhigh": "xhigh", "max": "max",
}

// claudeBudgetMode reports whether model predates adaptive thinking (Claude 3
// and the 4.0-4.5 models), where thinking needs a token budget.
func claudeBudgetMode(model string) bool {
	m := strings.ToLower(model)
	if strings.Contains(m, "claude-3") {
		return true
	}
	for _, v := range []string{"-4-5", "-4-1", "-4-0", "opus-4-2", "sonnet-4-2", "opus-4@", "sonnet-4@"} {
		if strings.Contains(m, v) {
			return true
		}
	}
	return m == "claude-opus-4" || m == "claude-sonnet-4"
}

// applyClaudeThinking adds the thinking parameters for level to body. ""
// and "auto" leave the model's default; "off" leaves them out as well, since
// several newer models think by default and reject {"type": "disabled"}.
// Budget mode also needs the interleaved-thinking beta (setThinkingBeta).
func applyClaudeThinking(body map[string]any, model, level string, maxTokensSet bool) {
	effort, ok := claudeEfforts[level]
	if !ok {
		return
	}
	if !maxTokensSet {
		body["max_tokens"] = claudeThinkingMaxTokens
	}
	if claudeBudgetMode(model) {
		budget := claudeBudgets[level]
		if max, _ := body["max_tokens"].(int); max > 0 && budget >= max {
			budget = max - 1 // budget_tokens must stay below max_tokens
		}
		body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": max(budget, 1024)}
		return
	}
	// summarized: newer models omit the thinking text by default; suag shows
	// and records it like other providers' reasoning.
	body["thinking"] = map[string]any{"type": "adaptive", "display": "summarized"}
	body["output_config"] = map[string]any{"effort": effort}
}

// claudeThinkingBlocks returns the thinking blocks stored on an assistant
// message (ClaudeThinkingMetaKey), nil when there are none or history
// folding removed the message's reasoning. Blocks read back from storage are
// []any of map[string]any; both that and []map[string]any are accepted.
func claudeThinkingBlocks(m agentcoreapi.Message) []map[string]any {
	if folded, _ := m.Metadata[ReasoningFoldedMetaKey].(bool); folded {
		return nil
	}
	var out []map[string]any
	switch list := m.Metadata[ClaudeThinkingMetaKey].(type) {
	case []any:
		for _, item := range list {
			if b, ok := item.(map[string]any); ok {
				out = append(out, b)
			}
		}
	case []map[string]any:
		out = list
	}
	return out
}
