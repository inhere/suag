package llm

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
)

const maxDebugBodyLen = 4000

func logProviderRequest(provider, path string, body map[string]any) {
	if !slog.Default().Enabled(context.Background(), slog.LevelDebug) {
		return
	}
	slog.Debug("llm request",
		"provider", strings.TrimSpace(provider),
		"path", strings.TrimSpace(path),
		"body", marshalDebugBody(body),
	)
}

func logProviderResponse(provider, path string, statusCode int, body []byte) {
	if !slog.Default().Enabled(context.Background(), slog.LevelDebug) {
		return
	}
	slog.Debug("llm response",
		"provider", strings.TrimSpace(provider),
		"path", strings.TrimSpace(path),
		"status_code", statusCode,
		"body", truncateDebugText(string(body)),
	)
}

// logStreamChunk logs one raw SSE data payload at debug level, so the exact
// field names a gateway streams (e.g. for reasoning) can be inspected.
func logStreamChunk(provider, payload string) {
	if !slog.Default().Enabled(context.Background(), slog.LevelDebug) {
		return
	}
	slog.Debug("llm stream chunk", "provider", strings.TrimSpace(provider), "data", truncateDebugText(payload))
}

func marshalDebugBody(body map[string]any) string {
	if len(body) == 0 {
		return "{}"
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "[marshal failed: " + err.Error() + "]"
	}
	return truncateDebugText(string(data))
}

func truncateDebugText(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= maxDebugBodyLen {
		return text
	}
	return text[:maxDebugBodyLen] + "...(truncated)"
}
