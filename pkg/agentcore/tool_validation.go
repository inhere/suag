package agentcore

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// prepareToolCalls checks the calls of one response against their schemas
// before anything runs: arguments in a common wrong shape are repaired
// (coerceArguments), and a call whose arguments do not fit the schema gets an
// ArgumentsError naming every problem at once, so it is answered with an error
// result and the other calls and the run go on (instead of failing the whole
// run). One rejected call costs the model one retry step, not a failed run.
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
		if issues := validateToolCallArguments(schema, *call); len(issues) > 0 {
			call.ArgumentsError = fmt.Sprintf("%v: %s; send the call again with the arguments the schema expects",
				ErrToolArgumentsInvalid, strings.Join(issues, "; "))
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
	// An optional field a strict-schema model fills with an empty placeholder
	// ("", [], {}) counts as not given: drop it so the tool reads its default
	// instead of a stand-in value (the same reading file_edit gives an empty
	// edits). A blank value of a required field stays and is reported as
	// missing by the validator below.
	required := make(map[string]struct{})
	for _, name := range requiredNames(schema.InputSchema) {
		required[name] = struct{}{}
	}
	for key, value := range args {
		if _, needed := required[key]; needed {
			continue
		}
		if emptyPlaceholder(value) {
			delete(args, key)
		}
	}
}

// emptyPlaceholder reports whether a value carries no content: null, a blank
// string, an empty array or an empty object. Models with strict tool schemas
// fill every optional field this way when they mean "not set".
func emptyPlaceholder(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	}
	return false
}

// requiredNames returns the field names of a schema's "required", in the
// declared order (hand-written schemas hold []any like JSON-decoded ones, but
// []string is accepted too). A required entry that is not a usable name is
// skipped rather than reported.
func requiredNames(schema map[string]any) []string {
	var names []string
	switch list := schema["required"].(type) {
	case []any:
		for _, item := range list {
			if name, ok := item.(string); ok && strings.TrimSpace(name) != "" {
				names = append(names, strings.TrimSpace(name))
			}
		}
	case []string:
		for _, name := range list {
			if strings.TrimSpace(name) != "" {
				names = append(names, strings.TrimSpace(name))
			}
		}
	}
	return names
}

// validateToolCallArguments checks one call against the tool's declared JSON
// schema and returns every problem it finds at once (nil when the call fits),
// so the model can fix the call in a single retry instead of one problem per
// round.
//
// It enforces only what the schema states unambiguously: the required fields,
// the declared type, enum and range (minimum/maximum, minLength/maxLength,
// minItems/maxItems) of a property, the same for array items and nested
// objects, and unknown fields when additionalProperties is false. A schema
// node without those keywords, or one relying on a construct this validator
// does not model (anyOf/oneOf/allOf/$ref/not), is left alone instead of
// guessed at: the schemas of MCP tools come from an external service and are
// often loose, and a loose third-party schema must never make suga refuse a
// call the server itself would accept. Only a call the schema clearly rejects
// is answered with an error result: the call does not run, the other calls of
// the response still do.
func validateToolCallArguments(schema ToolSchema, call ToolCall) []string {
	if len(schema.InputSchema) == 0 {
		return nil
	}
	validator := &schemaValidator{}
	validator.object("", call.Arguments, schema.InputSchema)
	return validator.issues
}

// schemaValidator collects every argument problem of one call.
type schemaValidator struct {
	issues []string
}

func (v *schemaValidator) addf(format string, args ...any) {
	v.issues = append(v.issues, fmt.Sprintf(format, args...))
}

// object validates the fields of one object value against its schema. prefix
// is empty for the call's arguments and a field path ("edits[0]") for an
// object nested in one, so every message names the field to fix. A nested
// object differs from the arguments themselves in one way: inside the call a
// blank string of a required field is a real value (file_edit's
// edits[0].new_string empties the matched run), while at the top level it has
// always counted as missing.
func (v *schemaValidator) object(prefix string, args map[string]any, schema map[string]any) {
	if unmodelledSchema(schema) {
		return
	}
	props, _ := schema["properties"].(map[string]any)
	for _, name := range requiredNames(schema) {
		value, given := args[name]
		if !given || (prefix == "" && emptyPlaceholder(value)) {
			v.addf("missing required argument %q", joinLocation(prefix, name))
		}
	}
	for _, name := range sortedKeys(args) {
		propSchema, declared := props[name].(map[string]any)
		if !declared {
			v.unknownField(prefix, name, props, schema)
			continue
		}
		v.value(joinLocation(prefix, name), args[name], propSchema)
	}
}

// unknownField reports a field the schema does not declare, but only when it
// says additionalProperties: false; every other schema passes extra fields
// through to the tool (which reports what it cannot use).
func (v *schemaValidator) unknownField(prefix, name string, props map[string]any, schema map[string]any) {
	additional, stated := schema["additionalProperties"].(bool)
	if !stated || additional {
		return
	}
	if len(props) == 0 {
		v.addf("unknown argument %q: this tool takes no arguments", joinLocation(prefix, name))
		return
	}
	v.addf("unknown argument %q: expected only %s", joinLocation(prefix, name), quoteList(sortedKeys(props)))
}

// value validates one value against its schema, following array items and
// nested objects down, so a wrong field inside e.g. edits[0] is named exactly.
// A null value is a placeholder of an optional field and is not checked.
func (v *schemaValidator) value(loc string, value any, schema map[string]any) {
	if value == nil || unmodelledSchema(schema) {
		return
	}
	v.checkType(loc, value, schema)
	v.checkEnum(loc, value, schema)
	switch typed := value.(type) {
	case float64:
		v.checkRange(loc, typed, schema)
	case string:
		v.checkLength(loc, typed, schema)
	case []any:
		v.checkSize(loc, typed, schema)
		if items, ok := schema["items"].(map[string]any); ok {
			for i := range typed {
				v.value(fmt.Sprintf("%s[%d]", loc, i), typed[i], items)
			}
		}
	case map[string]any:
		v.object(loc, typed, schema)
	}
}

// checkType reports a value whose JSON type is not one of the schema's.
func (v *schemaValidator) checkType(loc string, value any, schema map[string]any) {
	types := schemaTypes(schema["type"])
	if len(types) == 0 {
		return // no declared type: the schema constrains nothing here
	}
	for _, typ := range types {
		if matchesJSONType(value, typ) {
			return
		}
	}
	v.addf("argument %q expects %s, got %s", loc, strings.Join(types, " or "), jsonTypeName(value))
}

// checkEnum reports a value outside the schema's enum, naming the values the
// model may send.
func (v *schemaValidator) checkEnum(loc string, value any, schema map[string]any) {
	enum, ok := schema["enum"].([]any)
	if !ok || len(enum) == 0 {
		return
	}
	for _, candidate := range enum {
		if jsonValueEqual(value, candidate) {
			return
		}
	}
	v.addf("argument %q must be one of %s, got %s", loc, describeList(enum), describeValue(value))
}

// checkRange reports a number outside the schema's minimum/maximum.
func (v *schemaValidator) checkRange(loc string, value float64, schema map[string]any) {
	if min, ok := schemaNumber(schema["minimum"]); ok && value < min {
		v.addf("argument %q must be at least %v, got %v", loc, min, value)
	}
	if max, ok := schemaNumber(schema["maximum"]); ok && value > max {
		v.addf("argument %q must be at most %v, got %v", loc, max, value)
	}
}

// checkLength reports a string outside the schema's minLength/maxLength.
func (v *schemaValidator) checkLength(loc, value string, schema map[string]any) {
	if min, ok := schemaNumber(schema["minLength"]); ok && float64(len(value)) < min {
		v.addf("argument %q must be at least %d characters, got %d", loc, int(min), len(value))
	}
	if max, ok := schemaNumber(schema["maxLength"]); ok && float64(len(value)) > max {
		v.addf("argument %q must be at most %d characters, got %d", loc, int(max), len(value))
	}
}

// checkSize reports an array outside the schema's minItems/maxItems.
func (v *schemaValidator) checkSize(loc string, value []any, schema map[string]any) {
	if min, ok := schemaNumber(schema["minItems"]); ok && float64(len(value)) < min {
		v.addf("argument %q needs at least %d item(s), got %d", loc, int(min), len(value))
	}
	if max, ok := schemaNumber(schema["maxItems"]); ok && float64(len(value)) > max {
		v.addf("argument %q takes at most %d item(s), got %d", loc, int(max), len(value))
	}
}

// unmodelledSchema reports a schema node using a construct this validator does
// not model. Such a node is skipped whole (validation stays permissive) rather
// than reported wrongly: third-party schemas are free to use any JSON Schema
// feature.
func unmodelledSchema(schema map[string]any) bool {
	for _, key := range []string{"anyOf", "oneOf", "allOf", "not", "$ref", "if"} {
		if _, present := schema[key]; present {
			return true
		}
	}
	return false
}

// schemaTypes returns a schema's "type" as a list of names (a schema may
// declare several, e.g. ["string", "null"]).
func schemaTypes(raw any) []string {
	switch value := raw.(type) {
	case string:
		if value = strings.TrimSpace(value); value == "" || value == "any" {
			return nil
		}
		return []string{value}
	case []any:
		var types []string
		for _, item := range value {
			if name, ok := item.(string); ok && strings.TrimSpace(name) != "" {
				types = append(types, strings.TrimSpace(name))
			}
		}
		return types
	}
	return nil
}

// matchesJSONType reports whether a decoded JSON value has the named JSON type
// (an integral number matches "integer" as well as "number").
func matchesJSONType(value any, typ string) bool {
	if typ == "integer" {
		if number, ok := value.(float64); ok {
			return number == math.Trunc(number)
		}
	}
	return jsonTypeName(value) == typ
}

// jsonTypeName names the JSON type of a decoded value, as a model reading the
// error understands it; a value that is not JSON is named by its Go type.
func jsonTypeName(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		if v == math.Trunc(v) {
			return "integer" // a whole number is reported as the type models know
		}
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", value)
}

// schemaNumber reads a schema's numeric keyword (JSON numbers decode to
// float64, but a hand-written schema may hold an int).
func schemaNumber(raw any) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return f, true
		}
	}
	return 0, false
}

// jsonValueEqual compares two decoded JSON values, so an enum member and the
// argument can be matched (numbers are float64, arrays and objects compare by
// value).
func jsonValueEqual(a, b any) bool {
	if af, ok := a.(float64); ok {
		if bf, ok := b.(float64); ok {
			return af == bf
		}
		return false
	}
	return reflect.DeepEqual(a, b)
}

// sortedKeys returns a map's keys in sorted order: argument problems are
// reported in a stable order, whatever Go's map iteration does.
func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// joinLocation renders where an argument problem is: the field name at the top
// level, "edits[0]" for an array item, "edits[0].old_string" for a field of
// one.
func joinLocation(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

// quoteList renders names for an error message: "path", "content".
func quoteList(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, strconv.Quote(name))
	}
	return strings.Join(quoted, ", ")
}

// describeList renders schema values for an error message: "list", "output".
func describeList(values []any) string {
	rendered := make([]string, 0, len(values))
	for _, value := range values {
		rendered = append(rendered, describeValue(value))
	}
	return strings.Join(rendered, ", ")
}

// describeValue renders one value as the model wrote it: strings quoted, other
// scalars plainly.
func describeValue(value any) string {
	if text, ok := value.(string); ok {
		return strconv.Quote(text)
	}
	return fmt.Sprintf("%v", value)
}
