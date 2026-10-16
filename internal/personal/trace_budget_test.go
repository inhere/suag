package personal

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal/store"
)

func TestBudgetTraceRendering(t *testing.T) {
	event := store.Event{Kind: store.EventBudget, Name: "exceeded", Status: "ok", Attrs: map[string]any{
		"scope": "turn", "limit_tokens": float64(200), "used_tokens": float64(205), "remaining_tokens": float64(0),
		"limit_cost_usd": float64(2), "used_cost_usd": 2.1, "remaining_cost_usd": float64(0),
		"exceeded": true, "estimated": true, "reason": "max_tokens",
	}}
	t.Run("text names budget accounting", func(t *testing.T) {
		line := traceLine(event)
		assert.StrContains(t, line, "budget exceeded ok")
		assert.StrContains(t, line, "scope=turn")
		assert.StrContains(t, line, "tokens=200/205")
		assert.StrContains(t, line, "cost=$2.0000/$2.1000")
		assert.StrContains(t, line, "exceeded=true")
		assert.StrContains(t, line, "estimated=true")
	})
	t.Run("json keeps complete attrs for machine checks", func(t *testing.T) {
		var out bytes.Buffer
		assert.NoErr(t, WriteTrace(&out, []store.Event{event}, nil, true))
		var row map[string]any
		assert.NoErr(t, json.Unmarshal([]byte(strings.TrimSpace(out.String())), &row))
		attrs := row["attrs"].(map[string]any)
		assert.Eq(t, "turn", attrs["scope"])
		assert.Eq(t, true, attrs["estimated"])
		assert.Eq(t, float64(205), attrs["used_tokens"])
		assert.Eq(t, "max_tokens", attrs["reason"])
	})
}
