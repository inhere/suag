package personal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"unicode/utf8"

	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
)

// ErrContextOverflow ends a turn whose model context still exceeds the window
// after compaction and tool-result truncation. It is returned instead of
// retrying forever.
var ErrContextOverflow = errors.New("context window exceeded even after compaction and tool-result truncation")

// ctxItem is one context message together with the entries seq it was
// persisted under. Every message of a turn is persisted right after it is
// produced, so in-turn messages also have a seq (see turnSeqs).
type ctxItem struct {
	msg agentcore.Message
	seq int64
}

// historyView is the persisted part of a session's context: the latest adopted
// compaction summary and the message entries kept after it.
type historyView struct {
	summary string
	items   []ctxItem
}

// compactionRecord is the JSON body of a compaction entry.
//
// first_kept_seq is the entries seq of the first message that stays in the
// context. Both turn-start and mid-turn compactions use it: the view of a later
// turn is "summary + message entries of completed turns with seq >= first_kept_seq",
// which equals the in-memory view because a turn's messages are persisted as
// they are produced. A cut after the very last message uses lastSeq+1.
type compactionRecord struct {
	Summary      string `json:"summary"`
	FirstKeptSeq int64  `json:"first_kept_seq"`
	TokensBefore int    `json:"tokens_before"`
	TokensAfter  int    `json:"tokens_after"`
}

// loadHistory rebuilds the persisted context of a session. Only entries of
// completed turns count; a compaction is adopted only when it was written by a
// completed turn (or outside any turn, i.e. a manual :compact), so a failed or
// cancelled turn's compaction never leaks into later turns - exactly like its
// messages.
func (r *Runner) loadHistory(ctx context.Context, sessionID string) (historyView, error) {
	turns, err := r.cfg.Repository.ListTurns(ctx, sessionID, store.TurnCompleted)
	if err != nil {
		return historyView{}, fmt.Errorf("list completed turns: %w", err)
	}
	done := make(map[string]bool, len(turns))
	for _, t := range turns {
		done[t.ID] = true
	}
	entries, err := r.cfg.Repository.ListEntries(ctx, sessionID, store.EntryFilter{
		Types: []string{store.EntryUser, store.EntryAssistant, store.EntryTool, store.EntryCompaction, store.EntryHandoff},
	})
	if err != nil {
		return historyView{}, fmt.Errorf("load session history: %w", err)
	}
	var view historyView
	var firstKept int64
	for _, e := range entries { // ascending: the last adopted compaction wins
		if e.Type != store.EntryCompaction || (e.TurnID != "" && !done[e.TurnID]) {
			continue
		}
		var rec compactionRecord
		if err := json.Unmarshal(e.JSON, &rec); err != nil {
			return historyView{}, fmt.Errorf("decode compaction entry %d: %w", e.Seq, err)
		}
		view.summary, firstKept = rec.Summary, rec.FirstKeptSeq
	}
	for _, e := range entries {
		if e.Type == store.EntryCompaction || e.Seq < firstKept {
			continue
		}
		var m agentcore.Message
		if e.Type == store.EntryHandoff {
			// A handoff belongs to no turn and is the session's first context
			// message; once a compaction passes it, firstKept drops it (the
			// summary was built from it).
			var rec handoffRecord
			if err := json.Unmarshal(e.JSON, &rec); err != nil {
				return historyView{}, fmt.Errorf("decode handoff entry %d: %w", e.Seq, err)
			}
			view.items = append(view.items, ctxItem{msg: handoffMessage(rec), seq: e.Seq})
			continue
		}
		if !done[e.TurnID] {
			continue
		}
		if err := json.Unmarshal(e.JSON, &m); err != nil {
			return historyView{}, fmt.Errorf("decode history entry %d: %w", e.Seq, err)
		}
		view.items = append(view.items, ctxItem{msg: m, seq: e.Seq})
	}
	return view, nil
}

// summaryMessage is the first user message that carries a compaction summary.
func summaryMessage(summary string) agentcore.Message {
	return agentcore.Message{Role: "user", Content: "以下是此前对话的压缩摘要（早期消息已被省略）：\n\n" + summary}
}

// contextBuilder builds the model context of one turn: summary (if any) as the
// first user message, then kept history, the context notice, and this turn's
// messages. It runs before every model call (agentcore calls ContextBuilder in
// each step ahead of listTools/model), so the threshold check here also
// compacts a single very long turn. It never mutates state.Messages.
type contextBuilder struct {
	r         *Runner
	sessionID string
	turnID    string
	prompt    agentcore.PromptBuilder
	usage     *usageCollector
	update    string // short system notice (rules/append prompt/gofer context)

	summary   string    // current compaction summary ("" before any compaction)
	prev      []ctxItem // kept history before this turn's own messages
	turnStart int       // index of the first kept message in state.Messages
	turnSeqs  []int64   // entries seq of state.Messages[i], loaded lazily

	window, reserve, keep int
	truncLimit            int // tool-result truncation limit in chars; 0 = off

	fixedChars    int // system prompt + tool schemas, computed once
	haveFixed     bool
	lastViewChars int // chars of the view sent in the latest model call
	haveView      bool
}

func (r *Runner) newContextBuilder(sessionID, turnID string, hist historyView, prompt agentcore.PromptBuilder, usage *usageCollector, update string) *contextBuilder {
	window, reserve, keep := r.compactLimits()
	return &contextBuilder{
		r: r, sessionID: sessionID, turnID: turnID, prompt: prompt, usage: usage, update: update,
		summary: hist.summary, prev: hist.items, window: window, reserve: reserve, keep: keep,
	}
}

// threshold is the estimated size above which compaction triggers.
func (b *contextBuilder) threshold() int {
	return thresholdOf(b.window, b.reserve)
}

// BuildMessages implements agentcore.ContextBuilder.
func (b *contextBuilder) BuildMessages(ctx context.Context, state *agentcore.RunState) ([]agentcore.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	view := b.compose(state)
	est := b.estimate(ctx, state, view)
	if est > b.threshold() {
		out, err := b.compact(ctx, state, b.keep, false, est)
		switch {
		case err == nil:
			view = b.compose(state)
			_ = out
			est = b.estimate(ctx, state, view)
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case errors.Is(err, errNothingToCompact):
			// Nothing cuttable (e.g. one huge tool result): truncation below.
		default:
			// A failed summary must not kill the turn while truncation may still fit.
			slog.Warn("compact context", "session_id", b.sessionID, "err", err)
		}
	}
	if est > b.threshold() {
		var err error
		if view, err = b.shrink(ctx, state, view, &est); err != nil {
			return nil, err
		}
	}
	b.lastViewChars, b.haveView = charsOfMessages(view), true
	return view, nil
}

// compose assembles the current view: summary, kept history, notice, then the
// kept part of this turn's messages, with tool results truncated when needed.
func (b *contextBuilder) compose(state *agentcore.RunState) []agentcore.Message {
	var turn []agentcore.Message
	if state != nil && b.turnStart < len(state.Messages) {
		turn = state.Messages[b.turnStart:]
	}
	out := make([]agentcore.Message, 0, len(b.prev)+len(turn)+2)
	if b.summary != "" {
		out = append(out, summaryMessage(b.summary))
	}
	for _, it := range b.prev {
		out = append(out, b.limited(it.msg))
	}
	if b.update != "" {
		out = append(out, agentcore.Message{Role: "system", Content: b.update})
	}
	for _, m := range turn {
		out = append(out, b.limited(m))
	}
	return out
}

// limited returns a tool message with its content truncated to truncLimit; the
// stored entry (and state.Messages) keeps the full text.
func (b *contextBuilder) limited(m agentcore.Message) agentcore.Message {
	if b.truncLimit <= 0 || m.Role != "tool" {
		return m
	}
	if len(m.Content) > b.truncLimit {
		m.Content = truncateMiddle(m.Content, b.truncLimit)
	}
	if m.ToolResult != nil && len(m.ToolResult.Content) > b.truncLimit {
		res := *m.ToolResult
		res.Content = truncateMiddle(res.Content, b.truncLimit)
		m.ToolResult = &res
	}
	return m
}

// estimate approximates the tokens of the next model request: the previous
// call's reported input_tokens plus the chars added since that call's view
// (/4), or - before any call - system prompt + tool schemas + view (/4).
func (b *contextBuilder) estimate(ctx context.Context, state *agentcore.RunState, view []agentcore.Message) int {
	chars := charsOfMessages(view)
	if b.usage != nil && b.haveView {
		if last, ok := b.usage.lastInputTokens(); ok {
			return max(last+(chars-b.lastViewChars)/4, 0)
		}
	}
	return (b.fixed(ctx, state) + chars) / 4
}

func (b *contextBuilder) fixed(ctx context.Context, state *agentcore.RunState) int {
	if b.haveFixed {
		return b.fixedChars
	}
	b.haveFixed = true
	if b.prompt != nil && state != nil {
		if s, err := b.prompt.BuildSystemPrompt(ctx, state); err == nil {
			b.fixedChars += len(s)
		}
	}
	if b.r.cfg.Tools != nil {
		if schemas, err := b.r.cfg.Tools.ListTools(ctx); err == nil {
			if raw, err := json.Marshal(schemas); err == nil {
				b.fixedChars += len(raw)
			}
		}
	}
	return b.fixedChars
}

// truncation ladder for tool results: 16000 chars, halved down to 2000.
var truncateLadder = []int{16000, 8000, 4000, 2000}

// shrink truncates tool results (head+tail kept, UTF-8 safe) level by level
// until the view fits. It records a compaction error event; if even the
// smallest level does not fit it fails the turn with ErrContextOverflow.
func (b *contextBuilder) shrink(ctx context.Context, state *agentcore.RunState, view []agentcore.Message, est *int) ([]agentcore.Message, error) {
	before := *est
	for _, limit := range truncateLadder {
		if b.truncLimit != 0 && limit >= b.truncLimit {
			continue // already applied in a previous step
		}
		b.truncLimit = limit
		view = b.compose(state)
		*est = b.estimate(ctx, state, view)
		if *est <= b.threshold() {
			break
		}
	}
	fits := *est <= b.threshold()
	e := store.Event{
		Kind: store.EventCompaction, Name: "truncate", Status: eventError,
		Error: fmt.Sprintf("context estimate %d tokens exceeds threshold %d after compaction; tool results truncated to %d chars", before, b.threshold(), b.truncLimit),
		Attrs: map[string]any{"tokens_before": before, "tokens_after": *est, "truncate_limit": b.truncLimit, "fits": fits},
	}
	b.r.rec.record(ctx, e)
	if !fits {
		return nil, fmt.Errorf("%w (estimate %d, threshold %d)", ErrContextOverflow, *est, b.threshold())
	}
	return view, nil
}

// truncateMiddle keeps the head and tail of s within about limit bytes,
// cutting only on UTF-8 rune boundaries.
func truncateMiddle(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	half := limit / 2
	head := half
	for head > 0 && !utf8.RuneStart(s[head]) {
		head--
	}
	tail := len(s) - half
	for tail < len(s) && !utf8.RuneStart(s[tail]) {
		tail++
	}
	return fmt.Sprintf("%s\n...[truncated %d chars]...\n%s", s[:head], tail-head, s[tail:])
}

// charsOfMessages sums the model-visible size of messages.
func charsOfMessages(ms []agentcore.Message) int {
	n := 0
	for _, m := range ms {
		n += charsOfMessage(m)
	}
	return n
}

func charsOfMessage(m agentcore.Message) int {
	n := len(m.Content)
	if m.ToolResult != nil && len(m.ToolResult.Content) > n {
		n = len(m.ToolResult.Content) // tool messages carry the result twice; count once
	}
	if m.ToolCall != nil && m.Role != "tool" {
		n += len(m.ToolCall.Name)
		if raw, err := json.Marshal(m.ToolCall.Arguments); err == nil {
			n += len(raw)
		}
	}
	return n
}

// tokensOfMessage is the chars/4 approximation plus a small per-message overhead.
func tokensOfMessage(m agentcore.Message) int { return charsOfMessage(m)/4 + 4 }
