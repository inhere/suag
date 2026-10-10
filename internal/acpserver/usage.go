package acpserver

import (
	"github.com/coder/acp-go-sdk"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/pkg/agentcore"
)

// recordUsage returns the session-cumulative usage after a turn together with
// the context size of the last request.
//
// The cumulative total is owned by personal.Runner and persisted in session
// metadata, so it survives session/load and process restarts. When the turn
// never reached the runner's finish step (zero SessionUsage), the previously
// known total is kept; a turn without any model call (e.g. cancelled early)
// keeps the previous context size instead of resetting it to 0.
func (s *sessionState) recordUsage(a *Agent, result personal.RunResult) (agentcore.Usage, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if result.SessionUsage != (agentcore.Usage{}) {
		s.total = result.SessionUsage
	}
	if result.ModelCalls > 0 {
		s.lastContext = result.LastInputTokens
	}
	return s.total, s.lastContext
}

// usageUpdate builds the usage_update payload. _meta carries session-cumulative
// snake_case fields (clients may replace their totals with them) in the reported shape
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
