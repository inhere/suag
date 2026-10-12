package axconfig

import (
	"fmt"
	"log/slog"

	"github.com/inhere/suag/pkg/agentcore/llm"
)

// BuildProvider constructs the llm provider for one resolved model. The
// provider Name is the configured provider id so trace/stats record it.
func BuildProvider(spec ModelSpec) (llm.Provider, error) {
	p := spec.Provider
	switch p.Type {
	case ProviderTypeAnthropic:
		if level := spec.ThinkingLevel(); level != "" && level != "auto" {
			// Claude extended thinking needs its signed thinking blocks sent
			// back during tool use, which the Claude provider does not keep yet.
			slog.Warn("thinking levels are not supported for anthropic providers yet; ignored", "provider", p.ID, "thinking", level)
		}
		return llm.NewClaudeProvider(llm.ClaudeConfig{
			APIKey:  p.APIKey,
			BaseURL: p.BaseURL,
			Model:   spec.APIModel(),
			Headers: p.Headers,
			Name:    p.ID,
			// Claude cache breakpoints, see ProviderConfig.PromptCache.
			PromptCache: p.PromptCache,
		}), nil
	case ProviderTypeOpenAI:
		return llm.NewOpenAIProvider(llm.OpenAIConfig{
			APIKey:  p.APIKey,
			BaseURL: p.BaseURL,
			Model:   spec.APIModel(),
			Name:    p.ID,
			Headers: p.Headers,
			// Thinking level as reasoning_effort, see reasoningEffort.
			ReasoningEffort: reasoningEffort(spec.ThinkingLevel()),
		}), nil
	default:
		return nil, fmt.Errorf("providers.%s: unsupported type %q (want openai|anthropic)", p.ID, p.Type)
	}
}
