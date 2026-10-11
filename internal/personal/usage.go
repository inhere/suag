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
	// lastInput is the input_tokens of the most recent model call; the context
	// builder uses it to estimate the next request size.
	lastInput int
	// cost prices a call whose provider reported no cost (nil: leave as is).
	// It uses the turn's main model, so a fallback model's call is priced at
	// the main rate here; the per-attempt model events price it exactly.
	cost func(agentcore.Usage) float64
}

func (c *usageCollector) AfterModel(_ context.Context, event *agentcore.ModelEvent, _ *agentcore.Control) error {
	if event == nil || event.Result == nil {
		return nil
	}
	c.mu.Lock()
	usage := event.Result.Usage
	if usage.CostUSD == 0 && c.cost != nil {
		usage.CostUSD = c.cost(usage)
	}
	c.sum = c.sum.Add(usage)
	c.calls++
	c.lastInput = event.Result.Usage.InputTokens
	c.mu.Unlock()
	return nil
}

// lastInputTokens returns the latest call's input_tokens; ok is false before
// any call or when the provider reported none.
func (c *usageCollector) lastInputTokens() (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastInput, c.lastInput > 0
}

// snapshot returns the usage sum, the call count and the latest call's input tokens.
func (c *usageCollector) snapshot() (agentcore.Usage, int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sum, c.calls, c.lastInput
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
// result/input_required/error lines and ACP usage_update), in Anthropic-style
// accounting: input_tokens excludes cache, cache tokens are
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
