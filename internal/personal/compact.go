package personal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/inhere/suag/internal/personal/process"
	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
)

// Compaction defaults (tokens). Config can override them (mainly for tests).
const (
	defaultContextWindow = 128000
	defaultReserveTokens = 16000
	defaultKeepTokens    = 20000
)

// errNothingToCompact means no legal cut point exists that would drop anything.
var errNothingToCompact = errors.New("nothing to compact")

// compactSystemPrompt is the fixed summarizer instruction; tests match on it
// to tell summary calls from ordinary turns.
const compactSystemPrompt = "你是对话压缩助手。把给定的对话历史压缩成一份供同一个智能体继续工作的摘要。" +
	"只输出摘要正文，不要寒暄，不要调用工具。必须保留所有对继续工作有用的事实、路径、命令、标识符和数值。"

// compactTemplate is the fixed summary structure the summarizer must follow.
const compactTemplate = `请按下列固定结构输出摘要（没有内容的小节写“无”）：
## 目标
## 约束
## 进展
- 已完成：
- 进行中：
- 阻塞：
## 当前计划
## 关键决策
## 涉及文件
## 下一步`

// compactLimits resolves the window, reserve and keep budgets (tokens).
//
// Defaults scale with the window so small windows stay usable:
//
//	reserve   = min(16000, window*20%)
//	threshold = window - reserve        (window/2 when the reserve eats it all)
//	keep      = min(20000, threshold*40%)
//
// so a compacted context (~keep + summary) sits well below the threshold (about
// 60% or less) and the next turns have room before compacting again; a fixed
// keep close to the threshold made every turn compact and drop 1-2 messages.
// Explicit CompactReserveTokens/CompactKeepTokens win, except that keep is
// always capped at threshold*50% to keep that headroom.
func (r *Runner) compactLimits() (window, reserve, keep int) {
	window, reserve, keep = r.cfg.ContextWindow, r.cfg.CompactReserveTokens, r.cfg.CompactKeepTokens
	if window <= 0 {
		window = defaultContextWindow
	}
	if reserve <= 0 {
		reserve = min(defaultReserveTokens, window/5)
	}
	t := thresholdOf(window, reserve)
	if keep <= 0 {
		keep = min(defaultKeepTokens, t*2/5)
	}
	keep = min(keep, t/2)
	return window, reserve, keep
}

// thresholdOf is the estimated size above which compaction triggers.
func thresholdOf(window, reserve int) int {
	if t := window - reserve; t > 0 {
		return t
	}
	return window / 2
}

// validCut reports whether a view may start at items[i] (0 < i < len): either
// right before a user message, or right after a complete tool call/result pair
// (items[i-1] is a tool result and items[i] is not another result). It never
// separates an assistant{ToolCall} from its tool{ToolResult}; with parallel
// calls recorded as A1,T1,A2,T2 a cut between T1 and A2 is fine.
func validCut(items []ctxItem, i int) bool {
	cur, prev := items[i].msg, items[i-1].msg
	if cur.Role == "user" {
		return true
	}
	return prev.Role == "tool" && cur.Role != "tool"
}

// validEnd reports whether everything may be dropped (cut == len): the last
// message must close a pair or be a final assistant answer, never a user
// request that has not been answered.
func validEnd(items []ctxItem) bool {
	if len(items) == 0 {
		return false
	}
	last := items[len(items)-1].msg
	return last.Role == "tool" || (last.Role == "assistant" && last.ToolCall == nil)
}

// pickCut returns the index of the first kept item, or 0 when nothing can be
// dropped. It walks back from the tail until about keep tokens are covered and
// then aligns toward the end (keeping less, never more) to the nearest legal
// cut. allowEnd (manual compaction) also permits dropping everything.
func pickCut(items []ctxItem, keep int, allowEnd bool) int {
	n := len(items)
	start := n
	if keep > 0 {
		start = -1
		acc := 0
		for i := n - 1; i >= 0; i-- {
			if acc += tokensOfMessage(items[i].msg); acc >= keep {
				start = i
				break
			}
		}
		if start < 0 {
			// The whole context is below the keep budget yet compaction was
			// triggered (fixed prompt/tool overhead, or a tiny window): keep
			// less instead, from the first legal cut.
			start = 1
		}
	}
	for i := max(start, 1); i < n; i++ {
		if validCut(items, i) {
			return i
		}
	}
	if allowEnd && validEnd(items) {
		return n
	}
	return 0
}

// items lists the current view's messages (kept history + this turn's kept
// messages) with their entries seq.
func (b *contextBuilder) items(ctx context.Context, state *agentcore.RunState) ([]ctxItem, error) {
	out := append([]ctxItem(nil), b.prev...)
	if state == nil || b.turnStart >= len(state.Messages) {
		return out, nil
	}
	if len(b.turnSeqs) != len(state.Messages) {
		// The runtime persists every message right after producing it, so the
		// i-th message of the turn is the i-th message entry of the turn.
		entries, err := b.r.cfg.Repository.ListEntries(ctx, b.sessionID, store.EntryFilter{
			TurnID: b.turnID, Types: []string{store.EntryUser, store.EntryAssistant, store.EntryTool},
		})
		if err != nil {
			return nil, fmt.Errorf("list turn entries: %w", err)
		}
		if len(entries) != len(state.Messages) {
			return nil, fmt.Errorf("turn %s has %d stored messages but %d in memory", b.turnID, len(entries), len(state.Messages))
		}
		b.turnSeqs = b.turnSeqs[:0]
		for _, e := range entries {
			b.turnSeqs = append(b.turnSeqs, e.Seq)
		}
	}
	for i := b.turnStart; i < len(state.Messages); i++ {
		out = append(out, ctxItem{msg: state.Messages[i], seq: b.turnSeqs[i]})
	}
	return out, nil
}

type compactOutcome struct {
	tokensBefore, tokensAfter int
	firstKeptSeq              int64
}

// compact summarizes the oldest part of the current view, writes a compaction
// entry plus event, and switches this builder to the shorter view. before is
// the caller's token estimate of the view being compacted.
func (b *contextBuilder) compact(ctx context.Context, state *agentcore.RunState, keep int, manual bool, before int) (*compactOutcome, error) {
	items, err := b.items(ctx, state)
	if err != nil {
		return nil, err
	}
	cut := pickCut(items, keep, manual)
	if cut == 0 {
		return nil, errNothingToCompact
	}
	started := time.Now()
	trigger := "auto"
	if manual {
		trigger = "manual"
	}
	fail := func(err error) (*compactOutcome, error) {
		b.r.rec.record(ctx, store.Event{
			Kind: store.EventCompaction, Name: "compact", Status: eventError, Error: err.Error(),
			StartedAt: started, DurationMs: time.Since(started).Milliseconds(),
			Attrs: map[string]any{"trigger": trigger, "tokens_before": before},
		})
		return nil, err
	}
	summary, err := b.r.summarize(ctx, b.sessionID, b.summary, items[:cut], b.threshold())
	if err != nil {
		return fail(err)
	}
	firstKept := items[len(items)-1].seq + 1
	if cut < len(items) {
		firstKept = items[cut].seq
	}

	// Switch the view: cut inside the kept history drops its head; a cut inside
	// this turn drops all history and advances turnStart.
	oldSummary, oldPrev, oldStart := b.summary, b.prev, b.turnStart
	charsBefore := charsOfMessages(b.compose(state))
	b.summary = summary
	if cut <= len(oldPrev) {
		b.prev = oldPrev[cut:]
	} else {
		b.prev, b.turnStart = nil, oldStart+(cut-len(oldPrev))
	}
	after := max(before-(charsBefore-charsOfMessages(b.compose(state)))/4, 0)
	rec := compactionRecord{Summary: summary, FirstKeptSeq: firstKept, TokensBefore: before, TokensAfter: after}
	raw, _ := json.Marshal(rec)
	turnID := b.turnID
	if _, err := b.r.cfg.Repository.AppendEntries(context.WithoutCancel(ctx), b.sessionID, store.NewEntry{TurnID: turnID, Type: store.EntryCompaction, JSON: raw}); err != nil {
		b.summary, b.prev, b.turnStart = oldSummary, oldPrev, oldStart
		return fail(fmt.Errorf("write compaction entry: %w", err))
	}
	b.r.rec.record(ctx, store.Event{
		Kind: store.EventCompaction, Name: "compact", Status: eventOK,
		StartedAt: started, DurationMs: time.Since(started).Milliseconds(),
		Attrs: map[string]any{"trigger": trigger, "tokens_before": before, "tokens_after": after,
			"first_kept_seq": firstKept, "dropped_messages": cut, "model": b.r.compactModelName()},
	})
	// The prompt cache is rebuilt after a compaction anyway: refresh the
	// frozen memory index now (design D6) and use it from this step on.
	if b.r.refreshMemorySnapshot(ctx, b.sessionID, b.meta) {
		b.promptStale, b.haveFixed = true, false
	}
	return &compactOutcome{tokensBefore: before, tokensAfter: after, firstKeptSeq: firstKept}, nil
}

// compactProvider is the model used for summaries: llm.compact_model when
// configured, otherwise the turn's own (already observed) provider.
func (r *Runner) compactProvider() agentcore.Provider {
	if r.cfg.CompactProvider != nil {
		return r.cfg.CompactProvider
	}
	return r.cfg.Provider
}

func (r *Runner) compactModelName() string {
	if r.cfg.CompactProvider != nil && r.cfg.CompactModel != "" {
		return r.cfg.CompactModel
	}
	return r.cfg.Model
}

// summarize asks the model for a structured summary of dropped (plus the
// previous summary and the latest plan). The transcript is size-capped so the
// summary request itself fits the window.
func (r *Runner) summarize(ctx context.Context, sessionID, previous string, dropped []ctxItem, threshold int) (string, error) {
	var sb strings.Builder
	sb.WriteString(compactTemplate)
	if previous != "" {
		sb.WriteString("\n\n# 已有摘要（需并入新摘要）\n" + previous)
	}
	plan := "无"
	if e, err := r.cfg.Repository.LatestEntry(ctx, sessionID, store.EntryPlan); err == nil && e != nil && len(e.JSON) > 0 {
		plan = string(e.JSON)
	}
	sb.WriteString("\n\n# 当前计划（原样写入“当前计划”小节）\n" + plan)
	sb.WriteString("\n\n# 待压缩的对话\n")
	sb.WriteString(renderTranscript(dropped, max(threshold*3, 8000)))

	res, err := r.compactProvider().Generate(withPurpose(ctx, purposeCompact), &agentcore.GenerateRequest{
		System:   compactSystemPrompt,
		Messages: []agentcore.Message{{Role: "user", Content: sb.String()}},
	})
	if err != nil {
		return "", fmt.Errorf("summarize context: %w", err)
	}
	if res == nil || strings.TrimSpace(res.Content) == "" {
		return "", errors.New("summarize context: empty summary")
	}
	return strings.TrimSpace(res.Content), nil
}

// renderTranscript renders items as text, halving the per-item cap (4000 ->
// 500 chars) until the total fits maxChars.
func renderTranscript(items []ctxItem, maxChars int) string {
	var out string
	for limit := 4000; ; limit /= 2 {
		var sb strings.Builder
		for _, it := range items {
			m := it.msg
			switch {
			case m.Role == "tool":
				content := m.Content
				if m.ToolResult != nil && len(m.ToolResult.Content) > len(content) {
					content = m.ToolResult.Content
				}
				name := ""
				if m.ToolResult != nil {
					name = m.ToolResult.Name
				}
				fmt.Fprintf(&sb, "[tool_result %s] %s\n", name, truncateMiddle(content, limit))
			case m.ToolCall != nil:
				args, _ := json.Marshal(m.ToolCall.Arguments)
				fmt.Fprintf(&sb, "[assistant] %s\n[tool_call %s] %s\n", truncateMiddle(m.Content, limit), m.ToolCall.Name, truncateMiddle(string(args), limit))
			default:
				fmt.Fprintf(&sb, "[%s] %s\n", m.Role, truncateMiddle(m.Content, limit))
			}
		}
		out = sb.String()
		if len(out) <= maxChars || limit <= 500 {
			break
		}
	}
	return truncateMiddle(out, maxChars)
}

// CompactResult reports a manual compaction (token figures are estimates of the
// message context only, excluding system prompt and tool schemas).
type CompactResult struct {
	TokensBefore, TokensAfter int
	FirstKeptSeq              int64
}

// Compact compresses the session's persisted context right now (chat
// :compact). It drops everything older than the summary; the compaction entry
// belongs to no turn, so it is adopted immediately.
func (r *Runner) Compact(ctx context.Context, sessionID string) (CompactResult, error) {
	model, err := r.LoadSession(ctx, sessionID)
	if err != nil {
		return CompactResult{}, err
	}
	if model.Status == store.SessionArchived || model.Status == store.SessionRunning {
		return CompactResult{}, fmt.Errorf("session %q is not ready for compaction: %s", model.ID, model.Status)
	}
	r.locksMu.Lock()
	_, held := r.heldLocks[model.ID]
	r.locksMu.Unlock()
	if r.cfg.LockRoot != "" && !held {
		lock, err := process.AcquireSessionLockContext(ctx, r.cfg.LockRoot, model.ID, 30*time.Second)
		if err != nil {
			return CompactResult{}, err
		}
		defer lock.Close()
	}
	ctx = withObserveScope(ctx, model.ID, "")
	hist, err := r.loadHistory(ctx, model.ID)
	if err != nil {
		return CompactResult{}, err
	}
	b := r.newContextBuilder(model.ID, "", hist, nil, nil, "")
	before := charsOfMessages(b.compose(nil)) / 4
	out, err := b.compact(ctx, nil, 0, true, before)
	if errors.Is(err, errNothingToCompact) {
		return CompactResult{}, errors.New("nothing to compact")
	}
	if err != nil {
		return CompactResult{}, err
	}
	return CompactResult{TokensBefore: out.tokensBefore, TokensAfter: out.tokensAfter, FirstKeptSeq: out.firstKeptSeq}, nil
}
