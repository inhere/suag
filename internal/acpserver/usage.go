package acpserver

import (
	"context"
	"sync"

	"github.com/coder/acp-go-sdk"
	"github.com/inhere/suag/internal/personal"
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

// recordUsage returns the session-cumulative usage after a turn together with
// the context size of the last request.
//
// The cumulative total is owned by personal.Runner and persisted in session
// metadata, so it survives session/load and process restarts; persisted is
// RunResult.SessionUsage of the finished turn. When the turn never reached the
// runner's finish step (zero value), the previously known total is kept.
func (s *sessionState) recordUsage(a *Agent, turn *turnUsage, persisted agentcore.Usage) (agentcore.Usage, int) {
	_, last, calls := turn.snapshot()
	a.mu.Lock()
	defer a.mu.Unlock()
	if persisted != (agentcore.Usage{}) {
		s.total = persisted
	}
	// A turn without any model call (e.g. cancelled early) keeps the previous
	// context size instead of resetting it to 0.
	if calls > 0 {
		s.lastContext = last
	}
	return s.total, s.lastContext
}

// usageUpdate builds the usage_update payload. _meta carries session-cumulative
// snake_case fields (gofer replaces its totals with them) in the reported shape
// (input_tokens excludes cache, see personal.ReportUsage); used is the context
// size of the last request; size is the model context window.
func usageUpdate(total agentcore.Usage, used, size int) acp.SessionUpdate {
	rep := personal.ReportUsage(total)
	meta := map[string]any{
		"input_tokens": rep.InputTokens, "output_tokens": rep.OutputTokens,
		"total_tokens": rep.TotalTokens,
	}
	update := &acp.SessionUsageUpdate{Used: used, Size: size, Meta: meta}
	if rep.CacheReadTokens != 0 {
		meta["cache_read_tokens"] = rep.CacheReadTokens
	}
	if rep.CacheWriteTokens != 0 {
		meta["cache_write_tokens"] = rep.CacheWriteTokens
	}
	if rep.CostUSD != 0 {
		meta["cost_usd"] = rep.CostUSD
		update.Cost = &acp.Cost{Amount: rep.CostUSD, Currency: "USD"}
	}
	return acp.SessionUpdate{UsageUpdate: update}
}
