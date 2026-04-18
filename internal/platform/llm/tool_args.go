package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func decodeToolArguments(raw json.RawMessage, toolName string) (map[string]any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, fmt.Errorf("tool call %q arguments must be a JSON object", toolName)
	}

	var payload any
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return nil, fmt.Errorf("decode tool call %q arguments: %w", toolName, err)
	}

	args, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tool call %q arguments must be a JSON object", toolName)
	}
	return args, nil
}
