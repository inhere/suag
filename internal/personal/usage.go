package personal

import (
	"context"
	"sync"

	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
)

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

// usageFromSum converts the store's per-session model-event total back to the
// provider-normalized shape (input includes cache), so ReportUsage subtracts the
// cache part exactly as for a single turn. Total follows the same convention as
// providers: input + output.
func usageFromSum(s store.Usage) agentcore.Usage {
	return agentcore.Usage{
		InputTokens: int(s.InputTokens), OutputTokens: int(s.OutputTokens),
		TotalTokens:     int(s.InputTokens + s.OutputTokens),
		CacheReadTokens: int(s.CacheReadTokens), CacheWriteTokens: int(s.CacheWriteTokens),
		CostUSD: s.CostUSD,
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
