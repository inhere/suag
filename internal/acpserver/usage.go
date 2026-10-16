package acpserver

import (
	"github.com/coder/acp-go-sdk"
	"github.com/inhere/suga/internal/personal"
	"github.com/inhere/suga/pkg/agentcore"
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
func usageUpdate(run personal.RunUsage, session agentcore.Usage, used, size int, budget ...*personal.BudgetSnapshot) acp.SessionUpdate {
	meta := runUsageMeta(run)
	meta["run"] = runUsageMeta(run)
	meta["session"] = sessionUsageMeta(session)
	if len(budget) > 0 {
		addBudgetMeta(meta, budget[0])
	}
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

// addBudgetMeta exposes the optional budget view through ACP's reserved _meta
// object. Maps keep token and cost limits independent when both are configured,
// while the flat names remain easy for clients to consume.
func addBudgetMeta(meta map[string]any, snapshot *personal.BudgetSnapshot) {
	if snapshot == nil || snapshot.Scope == "" {
		return
	}
	limits := make(map[string]any)
	used := make(map[string]any)
	remaining := make(map[string]any)
	if snapshot.Tokens.Limit > 0 {
		limits["tokens"] = snapshot.Tokens.Limit
		used["tokens"] = snapshot.Tokens.Used
		remaining["tokens"] = snapshot.Tokens.Remaining
	}
	if snapshot.CostUSD.Limit > 0 {
		limits["cost_usd"] = snapshot.CostUSD.Limit
		used["cost_usd"] = snapshot.CostUSD.Used
		remaining["cost_usd"] = snapshot.CostUSD.Remaining
	}
	meta["budget_scope"] = snapshot.Scope
	meta["budget_limit"] = limits
	meta["budget_used"] = used
	meta["budget_remaining"] = remaining
	meta["budget_exceeded"] = snapshot.Exceeded
	if snapshot.Reason != "" {
		meta["budget_reason"] = snapshot.Reason
	}
	if snapshot.Estimated {
		meta["estimated"] = true
	}
}
