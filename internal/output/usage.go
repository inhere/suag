package output

import "github.com/inhere/suag/internal/personal"

// WithUsage adds usage to a stream-json line: "usage" is this turn, and
// "session_usage" the session-cumulative total. Both use the reported shape
// (input_tokens excludes cache; see personal.ReportUsage), identical on the
// result, input_required and error lines.
func WithUsage(line map[string]any, result personal.RunResult) map[string]any {
	line["usage"] = personal.ReportUsage(result.Usage)
	line["session_usage"] = personal.ReportUsage(result.SessionUsage)
	return line
}
