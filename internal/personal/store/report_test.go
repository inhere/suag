package store

import (
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestPercentileNearestRank(t *testing.T) {
	vals := []int64{50, 10, 40, 20, 30}
	assert.Eq(t, int64(30), Percentile(vals, 50))
	assert.Eq(t, int64(50), Percentile(vals, 95))
	assert.Eq(t, int64(10), Percentile(vals, 1))
	assert.Eq(t, int64(0), Percentile(nil, 50))
	assert.Eq(t, int64(50), vals[0]) // input untouched
}

func TestAggregateEventsInputFloor(t *testing.T) {
	rep := AggregateEvents([]Event{{Kind: EventModel, Name: "p", InputTokens: 100, CacheReadTokens: 90, CacheWriteTokens: 30,
		Attrs: map[string]any{"model": "m"}}})
	assert.Require(t, assert.Eq(t, 1, len(rep.Models)))
	assert.Eq(t, int64(0), rep.Models[0].InputTokens)
	assert.Eq(t, 90.0/120.0, rep.Models[0].CacheHitRate)
}
