package personal

import (
	"context"
	"sync"

	"github.com/inhere/suag/pkg/agentcore"
)

// usageMetadataKey is the session metadata key holding session-cumulative
// token usage. It lives in sessions.metadata_json, so no schema change is needed.
const usageMetadataKey = "usage"

// usageCollector sums the usage of every successful model call of one Run via
// the AfterModel hook. Unlike agentcore.RunResult.Usage it also covers turns
// that fail or are cancelled, and it never includes usage of an earlier
// invocation when a yielded run is resumed.
type usageCollector struct {
	agentcore.NopHook
	mu    sync.Mutex
	sum   agentcore.Usage
	calls int
}

func (c *usageCollector) AfterModel(_ context.Context, event *agentcore.ModelEvent, _ *agentcore.Control) error {
	if event == nil || event.Result == nil {
		return nil
	}
	c.mu.Lock()
	c.sum = AddUsage(c.sum, event.Result.Usage)
	c.calls++
	c.mu.Unlock()
	return nil
}

func (c *usageCollector) snapshot() (agentcore.Usage, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sum, c.calls
}

// AddUsage returns a+b field by field.
func AddUsage(a, b agentcore.Usage) agentcore.Usage {
	a.InputTokens += b.InputTokens
	a.OutputTokens += b.OutputTokens
	a.TotalTokens += b.TotalTokens
	a.CacheReadTokens += b.CacheReadTokens
	a.CacheWriteTokens += b.CacheWriteTokens
	a.CostUSD += b.CostUSD
	return a
}

// usageToMetadata encodes usage as a JSON-friendly map for session metadata.
func usageToMetadata(u agentcore.Usage) map[string]any {
	return map[string]any{
		"input_tokens": u.InputTokens, "output_tokens": u.OutputTokens, "total_tokens": u.TotalTokens,
		"cache_read_tokens": u.CacheReadTokens, "cache_write_tokens": u.CacheWriteTokens,
		"cost_usd": u.CostUSD,
	}
}

// usageFromMetadata decodes the persisted cumulative usage; a missing or
// malformed entry reads as zero. JSON numbers come back as float64, while a
// value set in-process may still be an int.
func usageFromMetadata(metadata map[string]any) agentcore.Usage {
	raw, _ := metadata[usageMetadataKey].(map[string]any)
	num := func(key string) float64 {
		switch v := raw[key].(type) {
		case float64:
			return v
		case int:
			return float64(v)
		}
		return 0
	}
	return agentcore.Usage{
		InputTokens: int(num("input_tokens")), OutputTokens: int(num("output_tokens")),
		TotalTokens:     int(num("total_tokens")),
		CacheReadTokens: int(num("cache_read_tokens")), CacheWriteTokens: int(num("cache_write_tokens")),
		CostUSD: num("cost_usd"),
	}
}

// ReportedUsage is the usage shape emitted on the output layer (stream-json
// result/input_required/error lines and ACP usage_update), matching Gofer's
// Anthropic-style accounting: input_tokens excludes cache, cache tokens are
// reported separately and total = input + output + cache_read + cache_write.
type ReportedUsage struct {
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	CacheReadTokens  int     `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int     `json:"cache_write_tokens,omitempty"`
	TotalTokens      int     `json:"total_tokens"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
}

// ReportUsage converts internal usage to the reported shape. The provider
// layer normalizes agentcore.Usage.InputTokens to INCLUDE cache read/write
// tokens (OpenAI prompt_tokens already contains cached_tokens; the Claude
// provider adds cache_read/cache_creation to input_tokens), so the cache part
// is subtracted here instead of changing the provider normalization.
func ReportUsage(u agentcore.Usage) ReportedUsage {
	input := max(u.InputTokens-u.CacheReadTokens-u.CacheWriteTokens, 0)
	return ReportedUsage{
		InputTokens: input, OutputTokens: u.OutputTokens,
		CacheReadTokens: u.CacheReadTokens, CacheWriteTokens: u.CacheWriteTokens,
		TotalTokens: input + u.OutputTokens + u.CacheReadTokens + u.CacheWriteTokens,
		CostUSD:     u.CostUSD,
	}
}
