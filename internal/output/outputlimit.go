package output

import "fmt"

// OutputLimitNotice is the text-mode stderr line of a turn whose answer was cut
// off at the model's output token limit (personal.RunResult.Truncated): the
// answer just stops, and the reason (a limit the configuration may raise) is
// invisible in the text itself. limit is how many tokens were allowed; 0 means
// neither the configuration nor the provider named one (an OpenAI-compatible
// server's own default applied).
func OutputLimitNotice(limit int) string {
	const fix = "；可用 max_output_tokens（模型或 provider）或 --max-output-tokens / SUGA_MAX_OUTPUT_TOKENS 提高上限"
	if limit > 0 {
		return fmt.Sprintf("本轮回答达到输出上限（%d tokens）而被截断，回答可能不完整%s", limit, fix)
	}
	return "本轮回答达到输出上限而被截断，回答可能不完整" + fix
}
