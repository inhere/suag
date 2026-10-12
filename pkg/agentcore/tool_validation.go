package agentcore

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// prepareToolCalls checks the calls of one response against their schemas
// before anything runs: arguments in a common wrong shape are repaired
// (coerceArguments), and a call that still misses a required argument gets an
// ArgumentsError, so it is answered with an error result and the other calls
// and the run go on (instead of failing the whole run).
func prepareToolCalls(schemas []ToolSchema, calls []ToolCall) {
	if len(schemas) == 0 || len(calls) == 0 {
		return
	}
	schemaByName := make(map[string]ToolSchema, len(schemas))
	for _, schema := range schemas {
		if name := strings.TrimSpace(schema.Name); name != "" {
			schemaByName[name] = schema
		}
	}
	for i := range calls {
		call := &calls[i]
		schema, ok := schemaByName[strings.TrimSpace(call.Name)]
		if !ok || call.ArgumentsError != "" {
			continue
		}
		coerceArguments(schema, call.Arguments)
		if err := validateToolCallRequiredArguments(schema, *call); err != nil {
			call.ArgumentsError = fmt.Sprintf("%v: %v; send the call again with every required argument", ErrToolArgumentsInvalid, err)
		}
	}
}

// coerceArguments repairs argument shapes models often get wrong, guided by
// the schema's property types: a JSON-encoded string where an array or object
// is expected is decoded, a numeric string becomes a number, "true"/"false"
// a boolean, and null values are dropped. Anything else is left as is (the
// tool reports what it cannot use).
func coerceArguments(schema ToolSchema, args map[string]any) {
	props, _ := schema.InputSchema["properties"].(map[string]any)
	for key, value := range args {
		if value == nil {
			delete(args, key)
			continue
		}
		prop, _ := props[key].(map[string]any)
		typ, _ := prop["type"].(string)
		text, isText := value.(string)
		if !isText {
			continue
		}
		trimmed := strings.TrimSpace(text)
		switch typ {
		case "array", "object":
			if (typ == "array" && strings.HasPrefix(trimmed, "[")) || (typ == "object" && strings.HasPrefix(trimmed, "{")) {
				var decoded any
				if json.Unmarshal([]byte(trimmed), &decoded) == nil {
					args[key] = decoded
				}
			}
		case "integer", "number":
			if n, err := strconv.ParseFloat(trimmed, 64); err == nil {
				args[key] = n
			}
		case "boolean":
			if b, err := strconv.ParseBool(trimmed); err == nil {
				args[key] = b
			}
		}
	}
}

func validateToolCallRequiredArguments(schema ToolSchema, call ToolCall) error {
	required, ok := schema.InputSchema["required"].([]any)
	if !ok || len(required) == 0 {
		return nil
	}

	for _, item := range required {
		name, _ := item.(string)
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if call.Arguments == nil {
			return fmt.Errorf("tool %q missing required argument %q", call.Name, name)
		}
		value, exists := call.Arguments[name]
		if !exists {
			return fmt.Errorf("tool %q missing required argument %q", call.Name, name)
		}
		if str, ok := value.(string); ok && strings.TrimSpace(str) == "" {
			return fmt.Errorf("tool %q missing required argument %q", call.Name, name)
		}
		if value == nil {
			return fmt.Errorf("tool %q missing required argument %q", call.Name, name)
		}
	}
	return nil
}
