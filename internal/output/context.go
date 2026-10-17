package output

import "fmt"

// ContextShare renders the last model request's context usage as "ctx 55%".
// It is empty when the model window or the request size is unknown. The ratio is
// intentionally not clamped, so an oversized request remains visible as >100%.
func ContextShare(lastInputTokens, contextWindow int) string {
	if contextWindow <= 0 || lastInputTokens <= 0 {
		return ""
	}
	return fmt.Sprintf("ctx %d%%", lastInputTokens*100/contextWindow)
}
