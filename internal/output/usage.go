package output

import "github.com/inhere/suga/internal/personal"

// WithUsage adds usage to a stream-json line: "usage" is this run's total
// (observe layer: every model call including compaction and failed attempts
// that reported usage, plus tool_calls), the same value as the last "usage"
// line; "session_usage" is the session-cumulative total. Both use the reported
// accounting (input_tokens excludes cache; see personal.ReportUsage), identical
// on the result, input_required and error lines.
func WithUsage(line map[string]any, result personal.RunResult) map[string]any {
	line["usage"] = personal.ReportRunUsage(result.RunUsage)
	line["session_usage"] = personal.ReportUsage(result.SessionUsage)
	return line
}
