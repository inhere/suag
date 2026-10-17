package output

import (
	"fmt"

	"github.com/inhere/suga/internal/personal"
)

// ContextUsage returns the last request's context size, model window and integer
// percentage. The result is absent when either input is unknown; callers should
// omit context information in that case.
func ContextUsage(lastInputTokens, contextWindow int) (used, window, share int, ok bool) {
	if contextWindow <= 0 || lastInputTokens <= 0 {
		return 0, 0, 0, false
	}
	return lastInputTokens, contextWindow, lastInputTokens * 100 / contextWindow, true
}

// ContextShare renders the last model request's context usage as "ctx 55%".
// It is empty when the model window or the request size is unknown. The ratio is
// intentionally not clamped, so an oversized request remains visible as >100%.
func ContextShare(lastInputTokens, contextWindow int) string {
	_, _, share, ok := ContextUsage(lastInputTokens, contextWindow)
	if !ok {
		return ""
	}
	return fmt.Sprintf("ctx %d%%", share)
}

// WithContext adds the optional context usage object to a stream-json line.
// used is the latest model request's input token count, including cache tokens;
// window is the effective model context window and share is its integer percent.
func WithContext(line map[string]any, result personal.RunResult, contextWindow int) map[string]any {
	used, window, share, ok := ContextUsage(result.LastInputTokens, contextWindow)
	if !ok {
		return line
	}
	line["context"] = map[string]any{"used": used, "window": window, "share": share}
	return line
}
