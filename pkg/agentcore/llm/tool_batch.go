package llm

import agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"

// groupToolBatches reorders each stored multi-call reply - its calls each
// followed by their result (call, result, call, result), all calls sharing
// api.ToolBatchMetaKey - into the calls followed by the results, which the
// request builders then merge into one assistant message with every call
// and the results after it: the parallel calls the model actually sent.
// Messages without the key, and a reply cut short (a call without its result
// right after it), are left as they are. The output depends on msgs alone,
// so an unchanged history renders the same request prefix every time.
func groupToolBatches(msgs []agentcoreapi.Message) []agentcoreapi.Message {
	out := make([]agentcoreapi.Message, 0, len(msgs))
	for i := 0; i < len(msgs); {
		batch := toolBatchOf(msgs[i])
		var calls, results []agentcoreapi.Message
		j := i
		for batch != "" && j+1 < len(msgs) && toolBatchOf(msgs[j]) == batch && isResultOf(msgs[j+1], msgs[j]) {
			calls, results = append(calls, msgs[j]), append(results, msgs[j+1])
			j += 2
		}
		if len(calls) < 2 {
			out = append(out, msgs[i])
			i++
			continue
		}
		out = append(append(out, calls...), results...)
		i = j
	}
	return out
}

// toolBatchOf returns the batch of an assistant tool-call message, "" for
// any other message.
func toolBatchOf(m agentcoreapi.Message) string {
	if m.Role != "assistant" || m.ToolCall == nil || m.ToolResult != nil {
		return ""
	}
	batch, _ := m.Metadata[agentcoreapi.ToolBatchMetaKey].(string)
	return batch
}

// isResultOf reports whether m is the tool result of the call message.
func isResultOf(m, call agentcoreapi.Message) bool {
	switch {
	case m.ToolResult != nil:
		return m.ToolResult.CallID == call.ToolCall.CallID
	case m.Role == "tool" && m.ToolCall != nil:
		return m.ToolCall.CallID == call.ToolCall.CallID
	}
	return false
}
