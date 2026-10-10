// Package llm implements first-party model providers for Suag.
package llm

import agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"

type Provider = agentcoreapi.Provider
type StreamingProvider = agentcoreapi.StreamingProvider
type GenerateRequest = agentcoreapi.GenerateRequest
type GenerateResult = agentcoreapi.GenerateResult
type FinishReason = agentcoreapi.FinishReason

const (
	FinishFinal     = agentcoreapi.FinishFinal
	FinishToolCalls = agentcoreapi.FinishToolCalls
)

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}

	dst := make(map[string]any, len(src))
	for k, v := range src {
		switch typed := v.(type) {
		case map[string]any:
			dst[k] = cloneMap(typed)
		case []any:
			items := make([]any, len(typed))
			for i, item := range typed {
				if nested, ok := item.(map[string]any); ok {
					items[i] = cloneMap(nested)
					continue
				}
				items[i] = item
			}
			dst[k] = items
		default:
			dst[k] = v
		}
	}
	return dst
}
