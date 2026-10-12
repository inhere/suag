package personal

import (
	"fmt"

	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/llm"
)

// foldArgChars is the size above which a string tool argument of a folded
// step is replaced by a short note (a whole file written, a long edit).
const foldArgChars = 1000

// foldHistory is the experimental history folding (context.fold_every,
// docs/research/2026-10-11-suag-vs-omp-loop-analysis.md, R3): in the request
// view, assistant steps before a boundary lose their reasoning and their long
// string arguments. The boundary only moves every `every` steps, keeping at
// least `keep` recent steps whole, so the cached request prefix changes once
// per batch rather than at every step. msgs is not modified; the stored
// history stays complete.
func foldHistory(msgs []agentcore.Message, every, keep int) []agentcore.Message {
	if every <= 0 {
		return msgs
	}
	steps := 0
	for _, m := range msgs {
		if m.Role == "assistant" {
			steps++
		}
	}
	boundary := (max(steps-keep, 0) / every) * every
	if boundary == 0 {
		return msgs
	}
	out := make([]agentcore.Message, len(msgs))
	copy(out, msgs)
	step := 0
	for i, m := range out {
		if m.Role != "assistant" {
			continue
		}
		if step >= boundary {
			break
		}
		step++
		if m.Reasoning != "" {
			// Keep the provider in thinking mode (llm.ReasoningFoldedMetaKey).
			meta := make(map[string]any, len(m.Metadata)+1)
			for k, v := range m.Metadata {
				meta[k] = v
			}
			meta[llm.ReasoningFoldedMetaKey] = true
			m.Metadata = meta
			m.Reasoning = ""
		}
		if m.ToolCall != nil {
			tc := *m.ToolCall
			tc.Arguments, _ = foldValue(tc.Arguments).(map[string]any)
			m.ToolCall = &tc
		}
		out[i] = m
	}
	return out
}

// foldValue returns v with every string longer than foldArgChars replaced by
// a note, descending into maps and lists (file_edit edits). It copies what it
// changes.
func foldValue(v any) any {
	switch x := v.(type) {
	case string:
		if len(x) > foldArgChars {
			return fmt.Sprintf("[folded: %d chars sent in an earlier step; re-read the file if you need its content]", len(x))
		}
		return x
	case map[string]any:
		if x == nil {
			return x
		}
		out := make(map[string]any, len(x))
		for k, item := range x {
			out[k] = foldValue(item)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = foldValue(item)
		}
		return out
	}
	return v
}
