package output

import "github.com/inhere/suga/internal/personal"

// WithUsage adds usage to a stream-json line: "usage" is this run's total
// (observe layer: every model call including compaction and failed attempts
// that reported usage, plus tool_calls), the same value as the last "usage"
// line; "session_usage" is the session-cumulative total. Both use the reported
// accounting (input_tokens excludes cache; see personal.ReportUsage), identical
// on the result, input_required and error lines. A budget snapshot is optional
// and is omitted for the historical no-budget path.
func WithUsage(line map[string]any, result personal.RunResult, budget ...*personal.BudgetSnapshot) map[string]any {
	line["usage"] = personal.ReportRunUsage(result.RunUsage)
	line["session_usage"] = personal.ReportUsage(result.SessionUsage)
	if len(budget) > 0 {
		addBudget(line, budget[0])
	}
	return line
}

// addBudget keeps the wire representation deliberately optional: no budget
// means no new keys, while an enabled budget only includes configured metrics.
func addBudget(line map[string]any, snapshot *personal.BudgetSnapshot) {
	if snapshot == nil || snapshot.Scope == "" {
		return
	}
	budget := map[string]any{"scope": snapshot.Scope, "exceeded": snapshot.Exceeded}
	if snapshot.Tokens.Limit > 0 {
		budget["tokens"] = snapshot.Tokens
	}
	if snapshot.CostUSD.Limit > 0 {
		budget["cost_usd"] = snapshot.CostUSD
	}
	if snapshot.Reason != "" {
		budget["reason"] = snapshot.Reason
	}
	line["budget"] = budget
	if snapshot.Estimated {
		line["estimated"] = true
	}
}
