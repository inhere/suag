package store

import (
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestAggregateEventsCountsEstimatedModelCalls(t *testing.T) {
	events := []Event{
		{Kind: EventModel, Name: "provider", Attrs: map[string]any{"model": "m", "estimated": true}},
		{Kind: EventModel, Name: "provider", Attrs: map[string]any{"model": "m", "estimated": false}},
		{Kind: EventModel, Name: "provider", Attrs: map[string]any{"model": "m"}},
	}
	t.Run("estimated calls are a separate optional count", func(t *testing.T) {
		rep := AggregateEvents(events)
		assert.Require(t, assert.Len(t, rep.Models, 1))
		assert.Eq(t, 3, rep.Models[0].Calls)
		assert.Eq(t, 1, rep.Models[0].EstimatedCalls)
	})
	t.Run("no estimated calls keeps the zero value", func(t *testing.T) {
		rep := AggregateEvents(events[1:])
		assert.Require(t, assert.Len(t, rep.Models, 1))
		assert.Eq(t, 0, rep.Models[0].EstimatedCalls)
	})
}
