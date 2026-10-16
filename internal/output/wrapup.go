package output

import (
	"fmt"

	"github.com/inhere/suga/internal/personal"
)

// WrapUpNotice is the text-mode stderr line of a turn whose answer was a
// wrap-up (result.WrapUp) rather than a normal one; it is empty for a normal
// answer. It names the real limit, because the answer itself is short for a
// reason a reader cannot see: without this line a wrapped-up answer is easily
// taken for a timeout or a finished task. stdout keeps the answer text only.
func WrapUpNotice(reason string, maxSteps int) string {
	switch reason {
	case "":
		return ""
	case personal.StopMaxSteps:
		if maxSteps > 0 {
			return fmt.Sprintf("本轮因步数上限（%d 步）收尾，以上答复只基于此前已获得的信息", maxSteps)
		}
		return "本轮因步数上限收尾，以上答复只基于此前已获得的信息"
	case personal.StopMaxTokens:
		return "本轮因 token 预算用尽收尾，以上答复只基于此前已获得的信息"
	case "max_cost_usd":
		return "本轮因成本预算用尽收尾，以上答复只基于此前已获得的信息"
	case personal.StopTimeout:
		return "本轮因时间上限收尾，以上答复只基于此前已获得的信息"
	default:
		return fmt.Sprintf("本轮因 %s 收尾，以上答复只基于此前已获得的信息", reason)
	}
}

// WrapUpTruncatedNotice calls out the less obvious combination where the
// budget's final wrap-up answer was itself cut by the per-request output cap.
func WrapUpTruncatedNotice(limit int) string {
	if limit > 0 {
		return fmt.Sprintf("本轮收尾答复同时达到输出上限（%d tokens）而被截断，回答可能不完整", limit)
	}
	return "本轮收尾答复同时达到输出上限而被截断，回答可能不完整"
}
