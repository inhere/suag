package agentcore

import (
	"fmt"
	"strings"
)

func validateToolCallsAgainstSchemas(schemas []ToolSchema, calls []ToolCall) error {
	if len(schemas) == 0 || len(calls) == 0 {
		return nil
	}

	schemaByName := make(map[string]ToolSchema, len(schemas))
	for _, schema := range schemas {
		name := strings.TrimSpace(schema.Name)
		if name == "" {
			continue
		}
		schemaByName[name] = schema
	}

	for _, call := range calls {
		schema, ok := schemaByName[strings.TrimSpace(call.Name)]
		if !ok || call.ArgumentsError != "" {
			continue // undecodable arguments are answered with an error result instead
		}
		if err := validateToolCallRequiredArguments(schema, call); err != nil {
			return &ToolError{Err: fmt.Errorf("%w: %v", ErrToolArgumentsInvalid, err)}
		}
	}
	return nil
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
