package llm

import (
	"encoding/json"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestDecodeToolArguments(t *testing.T) {
	t.Run("raw control characters in strings are escaped", func(t *testing.T) {
		raw := "{\"path\": \"a.go\", \"new_string\": \"func f() {\n\treturn\r\n}\"}"
		args, err := decodeToolArguments(json.RawMessage(raw), "file_edit")
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "func f() {\n\treturn\r\n}", args["new_string"])
	})
	t.Run("raw control characters inside an encoded arguments string", func(t *testing.T) {
		inner := "{\"edits\": [{\"old_string\": \"\ta\", \"new_string\": \"\tb\\\"q\\\"\"}]}"
		encoded, err := json.Marshal(inner) // the outer string itself is valid JSON
		assert.NoErr(t, err)
		args, err := decodeToolArguments(encoded, "file_edit")
		assert.Require(t, assert.NoErr(t, err))
		edit := args["edits"].([]any)[0].(map[string]any)
		assert.Eq(t, "\ta", edit["old_string"])
		assert.Eq(t, "\tb\"q\"", edit["new_string"])
	})
	t.Run("whitespace between tokens is untouched", func(t *testing.T) {
		args, err := decodeToolArguments(json.RawMessage("{\n\t\"a\": \"x\"\n}"), "t")
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "x", args["a"])
	})
	t.Run("other syntax errors keep the strict error", func(t *testing.T) {
		_, err := decodeToolArguments(json.RawMessage("{\"a\": \"x\t\""), "t")
		assert.ErrMsgContains(t, err, `decode tool call "t" arguments`)
		_, err = decodeToolArguments(json.RawMessage(`["a"]`), "t")
		assert.ErrMsgContains(t, err, "must be a JSON object")
	})
}
