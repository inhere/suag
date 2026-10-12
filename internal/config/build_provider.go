package axconfig

import (
	"fmt"

	"github.com/inhere/suag/pkg/agentcore/llm"
)

// BuildProvider constructs the llm provider for one resolved model. The
// provider Name is the configured provider id so trace/stats record it.
func BuildProvider(spec ModelSpec) (llm.Provider, error) {
	p := spec.Provider
	switch p.Type {
	case ProviderTypeAnthropic:
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
		}), nil
	default:
		return nil, fmt.Errorf("providers.%s: unsupported type %q (want openai|anthropic)", p.ID, p.Type)
	}
}
