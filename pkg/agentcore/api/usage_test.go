package api

import (
	"encoding/json"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestUsageJSONUsesSnakeCase(t *testing.T) {
	data, err := json.Marshal(Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5})
	assert.NoErr(t, err)
	assert.Eq(t, `{"input_tokens":3,"output_tokens":2,"total_tokens":5}`, string(data))
}

func TestUsageAddSumsEveryField(t *testing.T) {
	a := Usage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12, CacheReadTokens: 4, CacheWriteTokens: 1, CostUSD: 0.5}
	b := Usage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8, CacheReadTokens: 2, CacheWriteTokens: 1, CostUSD: 0.25}
	got := a.Add(b)
	want := Usage{InputTokens: 15, OutputTokens: 5, TotalTokens: 20, CacheReadTokens: 6, CacheWriteTokens: 2, CostUSD: 0.75}
	if got != want {
		t.Fatalf("Add = %+v, want %+v", got, want)
	}
	if a.InputTokens != 10 {
		t.Fatalf("Add must not modify the receiver, got %+v", a)
	}
}
