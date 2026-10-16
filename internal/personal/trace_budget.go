package personal

import (
	"fmt"
	"strings"

	"github.com/inhere/suga/internal/personal/store"
)

// budgetTraceParts renders the compact budget event view; JSON keeps the full
// attrs map, while text names the same scope, limits, usage and estimate flag.
func budgetTraceParts(e store.Event) []string {
	attrs := e.Attrs
	parts := []string{}
	if scope, _ := attrs["scope"].(string); scope != "" {
		parts = append(parts, "scope="+scope)
	}
	if limit, used := attrInt(attrs["limit_tokens"]), attrInt(attrs["used_tokens"]); limit > 0 {
		parts = append(parts, fmt.Sprintf("tokens=%d/%d remaining=%d", limit, used, attrInt(attrs["remaining_tokens"])))
	}
	if limit, used := attrFloat(attrs["limit_cost_usd"]), attrFloat(attrs["used_cost_usd"]); limit > 0 {
		parts = append(parts, fmt.Sprintf("cost=$%.4f/$%.4f remaining=$%.4f", limit, used, attrFloat(attrs["remaining_cost_usd"])))
	}
	if exceeded, ok := attrs["exceeded"].(bool); ok {
		parts = append(parts, fmt.Sprintf("exceeded=%t", exceeded))
	}
	if estimated, ok := attrs["estimated"].(bool); ok {
		parts = append(parts, fmt.Sprintf("estimated=%t", estimated))
	}
	if message, _ := attrs["message"].(string); message != "" {
		parts = append(parts, "message="+strings.Join(strings.Fields(message), " "))
	}
	return parts
}
