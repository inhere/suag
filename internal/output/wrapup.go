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
	case personal.StopTimeout:
		return "本轮因时间上限收尾，以上答复只基于此前已获得的信息"
	default:
		return fmt.Sprintf("本轮因 %s 收尾，以上答复只基于此前已获得的信息", reason)
	}
}
