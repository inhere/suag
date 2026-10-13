package llm

import "strings"

// DeepSeek models, directly or through a gateway:
//   - In thinking mode each earlier assistant turn must come back with its
//     reasoning (chat/completions: reasoning_content on the tool-call
//     messages, see withReasoning; Responses: a reasoning item with
//     reasoning_text, else 400 "The reasoning_text in the thinking mode must
//     be passed back to the API").
//   - The native usage reports cache hits as prompt_cache_hit_tokens; some
//     gateways pass only that one through.
func init() {
	registerCompat(compatAdapter{
		name: "deepseek",
		match: func(host, model string) bool {
			return host == "api.deepseek.com" || strings.Contains(model, "deepseek")
		},
		apply: func(c *Compat) {
			c.ReasoningEveryTurn = true
			c.CacheHitField = "prompt_cache_hit_tokens"
		},
	})
}

// cachedTokens is the prompt tokens served from cache in a chat/completions
// usage: the OpenAI field prompt_tokens_details.cached_tokens, or DeepSeek's
// prompt_cache_hit_tokens when a gateway passes only the native one through.
func cachedTokens(openai, deepseek int) int {
	if openai > 0 {
		return openai
	}
	return deepseek
}
