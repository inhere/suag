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

// lastContextSize is the context size of the previous turn's last request.
func (s *sessionState) lastContextSize(a *Agent) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return s.lastContext
}

// usageUpdate builds the usage_update payload, sent after every model call and
// once more at the end of the turn. _meta carries, in the reported shape
// (input_tokens excludes cache, see personal.ReportUsage):
//   - flat snake_case keys (input_tokens, output_tokens, cache_read_tokens,
//     cache_write_tokens, total_tokens, cost_usd, tool_calls): the run total of
//     this session/prompt so far, always present. Gofer reads only these keys,
//     so its budget is per run;
//   - "run": the same run total as a nested object;
//   - "session": the session-cumulative total (earlier turns plus this run).
//
// used is the context size of the latest request; size is the model context
// window; cost (the ACP field) stays session-cumulative.
func usageUpdate(run personal.RunUsage, session agentcore.Usage, used, size int) acp.SessionUpdate {
	meta := runUsageMeta(run)
	meta["run"] = runUsageMeta(run)
	meta["session"] = sessionUsageMeta(session)
	update := &acp.SessionUsageUpdate{Used: used, Size: size, Meta: meta}
	if cost := personal.ReportUsage(session).CostUSD; cost != 0 {
		update.Cost = &acp.Cost{Amount: cost, Currency: "USD"}
	}
	return acp.SessionUpdate{UsageUpdate: update}
}

func runUsageMeta(run personal.RunUsage) map[string]any {
	rep := personal.ReportRunUsage(run)
	return map[string]any{
		"input_tokens": rep.InputTokens, "output_tokens": rep.OutputTokens,
		"cache_read_tokens": rep.CacheReadTokens, "cache_write_tokens": rep.CacheWriteTokens,
		"total_tokens": rep.TotalTokens, "cost_usd": rep.CostUSD, "tool_calls": rep.ToolCalls,
	}
}

func sessionUsageMeta(session agentcore.Usage) map[string]any {
	rep := personal.ReportUsage(session)
	return map[string]any{
		"input_tokens": rep.InputTokens, "output_tokens": rep.OutputTokens,
		"cache_read_tokens": rep.CacheReadTokens, "cache_write_tokens": rep.CacheWriteTokens,
		"total_tokens": rep.TotalTokens, "cost_usd": rep.CostUSD,
	}
}
