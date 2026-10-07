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
