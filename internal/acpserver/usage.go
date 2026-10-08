package acpserver

import (
	"context"
	"sync"

	"github.com/coder/acp-go-sdk"
	"github.com/inhere/suag/pkg/agentcore"
)

// turnUsage collects per-model-call usage during one prompt turn through the
// AfterModel hook. personal.RunResult only exposes the turn sum, which cannot
// tell the context size of the last request, and is empty for a cancelled turn.
type turnUsage struct {
	mu    sync.Mutex
	sum   agentcore.Usage
	last  int // input tokens (cache included) of the most recent model request
	calls int
}

func (t *turnUsage) add(u agentcore.Usage) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sum.InputTokens += u.InputTokens
	t.sum.OutputTokens += u.OutputTokens
	t.sum.TotalTokens += u.TotalTokens
	t.sum.CacheReadTokens += u.CacheReadTokens
	t.sum.CacheWriteTokens += u.CacheWriteTokens
	t.sum.CostUSD += u.CostUSD
	t.last = u.InputTokens
	t.calls++
}

func (t *turnUsage) snapshot() (agentcore.Usage, int, int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sum, t.last, t.calls
}

// AfterModel records usage of each successful model call of the turn.
func (h *permissionHook) AfterModel(_ context.Context, event *agentcore.ModelEvent, _ *agentcore.Control) error {
	if h.usage != nil && event != nil && event.Result != nil {
		h.usage.add(event.Result.Usage)
	}
	return nil
}

// recordUsage folds a finished turn into the session totals and returns the
// totals together with the context size of the last request.
//
// Session totals live only in the ACP process: personal sessions do not persist
// usage, so after session/load (or a restart) accumulation resumes from zero.
func (s *sessionState) recordUsage(a *Agent, turn *turnUsage) (agentcore.Usage, int) {
	sum, last, calls := turn.snapshot()
	a.mu.Lock()
	defer a.mu.Unlock()
	t := &s.total
	t.InputTokens += sum.InputTokens
	t.OutputTokens += sum.OutputTokens
	t.TotalTokens += sum.TotalTokens
	t.CacheReadTokens += sum.CacheReadTokens
	t.CacheWriteTokens += sum.CacheWriteTokens
	t.CostUSD += sum.CostUSD
	// A turn without any model call (e.g. cancelled early) keeps the previous
	// context size instead of resetting it to 0.
	if calls > 0 {
		s.lastContext = last
	}
	return *t, s.lastContext
}

// usageUpdate builds the usage_update payload. _meta carries session-cumulative
// snake_case fields (gofer replaces its totals with them); used is the context
// size of the last request; size is the model context window.
func usageUpdate(total agentcore.Usage, used, size int) acp.SessionUpdate {
	meta := map[string]any{
		"input_tokens": total.InputTokens, "output_tokens": total.OutputTokens,
		"total_tokens": total.TotalTokens,
	}
	update := &acp.SessionUsageUpdate{Used: used, Size: size, Meta: meta}
	if total.CacheReadTokens != 0 {
		meta["cache_read_tokens"] = total.CacheReadTokens
	}
	if total.CacheWriteTokens != 0 {
		meta["cache_write_tokens"] = total.CacheWriteTokens
	}
	if total.CostUSD != 0 {
		meta["cost_usd"] = total.CostUSD
		update.Cost = &acp.Cost{Amount: total.CostUSD, Currency: "USD"}
	}
	return acp.SessionUpdate{UsageUpdate: update}
}
