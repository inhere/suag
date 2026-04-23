package llm

import (
	"encoding/json"
	"log/slog"
	"strings"
)

const maxDebugBodyLen = 4000

func logProviderRequest(provider, path string, body map[string]any) {
	if !slog.Default().Enabled(nil, slog.LevelDebug) {
		return
	}
	slog.Debug("llm request",
		"provider", strings.TrimSpace(provider),
		"path", strings.TrimSpace(path),
		"body", marshalDebugBody(body),
	)
}

func logProviderResponse(provider, path string, statusCode int, body []byte) {
	if !slog.Default().Enabled(nil, slog.LevelDebug) {
		return
	}
	slog.Debug("llm response",
		"provider", strings.TrimSpace(provider),
		"path", strings.TrimSpace(path),
		"status_code", statusCode,
		"body", truncateDebugText(string(body)),
	)
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
