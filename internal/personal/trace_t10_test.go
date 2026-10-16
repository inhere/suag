package personal

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal/store"
)

func TestApprovalRuleTraceTextAndJSON(t *testing.T) {
	event := store.Event{Kind: store.EventDenial, Name: "act", Status: eventDenied, Attrs: map[string]any{
		"approval":       "rule",
		"approval_rule":  `project#2 file_write path="**/*.env" action=deny`,
		"approval_scope": "file_write:config/prod.env",
	}}
	var text bytes.Buffer
	assert.NoErr(t, WriteTrace(&text, []store.Event{event}, nil, false))
	assert.StrContains(t, text.String(), `approval=rule(project#2: file_write "**/*.env")`)

	var encoded bytes.Buffer
	assert.NoErr(t, WriteTrace(&encoded, []store.Event{event}, nil, true))
	var row map[string]any
	assert.Require(t, assert.NoErr(t, json.Unmarshal([]byte(strings.TrimSpace(encoded.String())), &row)))
	attrs, ok := row["attrs"].(map[string]any)
	assert.Require(t, assert.True(t, ok))
	assert.Eq(t, "project#2 file_write path=\"**/*.env\" action=deny", attrs["approval_rule"])
	assert.Eq(t, "file_write:config/prod.env", attrs["approval_scope"])
}
