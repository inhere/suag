package main

import (
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestFirstMainCall(t *testing.T) {
	t.Run("skips plan errors and compact", func(t *testing.T) {
		trace := `{"kind":"plan","plan":[]}
{"kind":"model","status":"error","input_tokens":999}
{"kind":"model","status":"ok","input_tokens":500,"attrs":{"purpose":"compact"}}
{"kind":"model","status":"ok","input_tokens":1000,"cache_read_tokens":700,"cache_write_tokens":100,"attrs":{"attempt":1}}
{"kind":"model","status":"ok","input_tokens":1200,"cache_read_tokens":1000}
`
		c, ok := firstMainCall([]byte(trace))
		assert.Require(t, assert.Eq(t, true, ok))
		assert.Eq(t, int64(200), c.uncached())
		assert.Eq(t, int64(700), c.CacheRead)
	})
	t.Run("none", func(t *testing.T) {
		_, ok := firstMainCall([]byte(`{"kind":"tool","status":"ok"}` + "\n"))
		assert.Eq(t, false, ok)
	})
	t.Run("uncached never negative", func(t *testing.T) {
		assert.Eq(t, int64(0), traceCall{Input: 10, CacheRead: 20}.uncached())
	})
}

func TestAggregateProbe(t *testing.T) {
	rs := []ProbeRun{
		{Group: "same", Input: 1000, CacheRead: 800, Uncached: 200, WarmInput: 1000},
		{Group: "same", Input: 1000, CacheRead: 600, Uncached: 400, WarmInput: 1000},
		{Group: "same", Error: "boom"},
		{Group: "control", Input: 1000, Uncached: 1000, WarmInput: 1000},
	}
	m := aggregateProbe(rs)
	t.Run("same skips failed runs", func(t *testing.T) {
		s := m["same"]
		assert.Eq(t, 3, s.n)
		assert.Eq(t, 1, s.failed)
		assert.Eq(t, 1400.0, s.cacheRead)
		assert.Eq(t, 1.4, s.ratio)
	})
	t.Run("control has no hits", func(t *testing.T) {
		assert.Eq(t, 0.0, m["control"].cacheRead)
	})
}
