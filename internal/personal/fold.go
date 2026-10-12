package personal

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/llm"
)

// foldArgChars is the size above which a string tool argument of a folded
// step is replaced by a short note (a whole file written, a long edit).
const foldArgChars = 1000

// foldHistory is history folding (context.fold_every / context.fold_keep,
// docs/design/2026-10-12-history-folding-design.md): in the request view,
// assistant steps before a boundary lose their reasoning and their long
// string arguments. The boundary only moves every `every` steps, keeping at
// least `keep` recent steps whole, so the cached request prefix changes once
// per batch rather than at every step. msgs is not modified; the stored
// history stays complete.
func foldHistory(msgs []agentcore.Message, every, keep int) []agentcore.Message {
	boundary, _ := foldBoundary(msgs, every, keep)
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

// foldBoundary returns how many leading assistant steps of msgs are folded
// and how many steps there are: ((steps - keep) / every) * every, 0 when
// folding is off (every <= 0).
func foldBoundary(msgs []agentcore.Message, every, keep int) (boundary, steps int) {
	for _, m := range msgs {
		if m.Role == "assistant" {
			steps++
		}
	}
	if every <= 0 {
		return 0, steps
	}
	return (max(steps-keep, 0) / every) * every, steps
}

// metaFoldBoundary is the session meta key holding the fold boundary last
// recorded as a fold event in the current segment (reset by segmentMeta).
const metaFoldBoundary = "fold_boundary"

// metaCount reads a non-negative count stored in session meta (JSON numbers
// come back as float64); anything else is 0.
func metaCount(v any) int {
	switch n := v.(type) {
	case float64:
		return max(int(n), 0)
	case int:
		return max(n, 0)
	case int64:
		return max(int(n), 0)
	}
	return 0
}

// foldStats describes a fold boundary move waiting to be recorded.
type foldStats struct {
	boundary, steps         int
	charsBefore, charsAfter int
}

// recordFold writes the pending boundary move (set by compose) as a fold
// event and stores the new baseline in the session meta, so a later turn's
// builder does not record it again. Best-effort, like other events.
func (b *contextBuilder) recordFold(ctx context.Context) {
	f := b.foldPending
	b.foldPending = nil
	if f == nil || f.boundary <= b.foldBase {
		return
	}
	b.foldBase = f.boundary
	b.r.rec.record(ctx, store.Event{
		Kind: store.EventFold, Name: "fold", Status: eventOK, StartedAt: time.Now(),
		Attrs: map[string]any{"boundary": f.boundary, "steps": f.steps,
			"chars_before": f.charsBefore, "chars_after": f.charsAfter},
	})
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := b.r.cfg.Repository.PatchSessionMeta(wctx, b.sessionID, map[string]any{metaFoldBoundary: f.boundary}); err != nil {
		slog.Warn("store fold boundary in session meta", "session_id", b.sessionID, "err", err)
	}
}

// foldValue returns v with every string longer than foldArgChars replaced by
// a note, descending into maps and lists (file_edit edits). It copies what it
// changes.
func foldValue(v any) any {
	switch x := v.(type) {
	case string:
		if len(x) > foldArgChars {
			// No "re-read" hint: it invited reads the step did not need.
			return fmt.Sprintf("[folded: %d chars sent in an earlier step]", len(x))
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
