package llm

import (
	"bytes"
	"encoding/json"
	"fmt"

	agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"
)

// newToolCall builds the tool call the model requested. Arguments that cannot
// be decoded do not fail the response: the call carries the decode error in
// ArgumentsError (with empty arguments) and the runtime answers it with an
// error result, so one malformed call costs a step instead of the whole run.
func newToolCall(id, name string, raw json.RawMessage) agentcoreapi.ToolCall {
	args, err := decodeToolArguments(raw, name)
	if err != nil {
		return agentcoreapi.ToolCall{CallID: id, Name: name, Arguments: map[string]any{}, ArgumentsError: err.Error()}
	}
	return agentcoreapi.ToolCall{CallID: id, Name: name, Arguments: args}
}

func decodeToolArguments(raw json.RawMessage, toolName string) (map[string]any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, fmt.Errorf("tool call %q arguments must be a JSON object", toolName)
	}

	var payload any
	if err := unmarshalLenient(trimmed, &payload); err != nil {
		return nil, fmt.Errorf("decode tool call %q arguments: %w", toolName, err)
	}

	// OpenAI-compatible providers often return function.arguments as a JSON
	// string, while Claude-style providers return an inline object. Accept both
	// shapes here so upper layers can stay provider-agnostic.
	if encoded, ok := payload.(string); ok {
		encoded = string(bytes.TrimSpace([]byte(encoded)))
		if encoded == "" || encoded == "null" {
			return nil, fmt.Errorf("tool call %q arguments must be a JSON object", toolName)
		}
		if err := unmarshalLenient([]byte(encoded), &payload); err != nil {
			return nil, fmt.Errorf("decode tool call %q arguments string: %w", toolName, err)
		}
	}

	args, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tool call %q arguments must be a JSON object", toolName)
	}
	return args, nil
}

// unmarshalLenient is json.Unmarshal that, when strict decoding fails, retries
// once with raw control characters inside string literals escaped. Models
// writing code often emit literal tabs or newlines in argument strings (Go
// indentation), which strict JSON rejects; the escaped text is exactly what
// the model meant. The strict error is returned if the retry fails too.
func unmarshalLenient(data []byte, v any) error {
	err := json.Unmarshal(data, v)
	if err == nil {
		return nil
	}
	if fixed, changed := escapeControlInStrings(data); changed && json.Unmarshal(fixed, v) == nil {
		return nil
	}
	return err
}

// escapeControlInStrings escapes bytes below 0x20 that appear inside JSON
// string literals, tracking quotes and backslash escapes; bytes outside
// strings (insignificant whitespace) are left alone.
func escapeControlInStrings(data []byte) ([]byte, bool) {
	var out bytes.Buffer
	inString, escaped, changed := false, false, false
	for _, c := range data {
		switch {
		case !inString:
			inString = c == '"'
		case escaped:
			escaped = false
		case c == '\\':
			escaped = true
		case c == '"':
			inString = false
		case c < 0x20:
			changed = true
			switch c {
			case '\t':
				out.WriteString(`\t`)
			case '\n':
				out.WriteString(`\n`)
			case '\r':
				out.WriteString(`\r`)
			default:
				fmt.Fprintf(&out, `\u%04x`, c)
			}
			continue
		}
		out.WriteByte(c)
	}
	return out.Bytes(), changed
}
