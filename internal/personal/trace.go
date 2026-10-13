package personal

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore/tools/native"
)

// traceEvent is the jsonl shape of one timeline event.
type traceEvent struct {
	Time         string         `json:"time"`
	Kind         string         `json:"kind"`
	Name         string         `json:"name"`
	Status       string         `json:"status"`
	TurnID       string         `json:"turn_id"`
	DurationMs   int64          `json:"duration_ms"`
	TTFTMs       int64          `json:"ttft_ms,omitempty"`
	InputTokens  int64          `json:"input_tokens,omitempty"`
	OutputTokens int64          `json:"output_tokens,omitempty"`
	CacheRead    int64          `json:"cache_read_tokens,omitempty"`
	CacheWrite   int64          `json:"cache_write_tokens,omitempty"`
	Error        string         `json:"error,omitempty"`
	Attrs        map[string]any `json:"attrs,omitempty"`
}

// tracePlan is the jsonl shape of one update_plan record in the timeline.
type tracePlan struct {
	Time        string     `json:"time"`
	Kind        string     `json:"kind"`
	TurnID      string     `json:"turn_id"`
	Explanation string     `json:"explanation,omitempty"`
	Plan        []planStep `json:"plan"`
}

type planStep struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}

// planSymbol maps a plan step status to its marker.
func planSymbol(status string) string {
	switch status {
	case native.PlanCompleted:
		return "✔"
	case native.PlanInProgress:
		return "▶"
	default:
		return "○"
	}
}

// traceRow is one timeline item: an event or a plan entry, ordered by time.
type traceRow struct {
	at    time.Time
	event *store.Event
	plan  *store.Entry
}

// mergeTrace interleaves events (already oldest first) with plan entries
// (ordered by seq, so also by creation time) by timestamp. On equal times the
// event goes first, so a plan update follows the tool event that produced it.
func mergeTrace(events []store.Event, plans []store.Entry) []traceRow {
	rows := make([]traceRow, 0, len(events)+len(plans))
	i, j := 0, 0
	for i < len(events) || j < len(plans) {
		if j >= len(plans) || (i < len(events) && !plans[j].CreatedAt.Before(events[i].StartedAt)) {
			rows = append(rows, traceRow{at: events[i].StartedAt, event: &events[i]})
			i++
			continue
		}
		rows = append(rows, traceRow{at: plans[j].CreatedAt, plan: &plans[j]})
		j++
	}
	return rows
}

// WriteTrace prints events and plan entries (oldest first) as a timeline: text
// mode is one line per event (and one per plan step), asJSON writes one JSON
// object per line (kind "plan" for a plan entry).
func WriteTrace(w io.Writer, events []store.Event, plans []store.Entry, asJSON bool) error {
	enc := json.NewEncoder(w)
	for _, row := range mergeTrace(events, plans) {
		if row.plan != nil {
			if err := writePlanRow(w, enc, *row.plan, asJSON); err != nil {
				return err
			}
			continue
		}
		e := *row.event
		if asJSON {
			if err := enc.Encode(traceEvent{
				Time: e.StartedAt.UTC().Format(time.RFC3339Nano), Kind: e.Kind, Name: e.Name, Status: e.Status,
				TurnID: e.TurnID, DurationMs: e.DurationMs, TTFTMs: e.TTFTMs,
				InputTokens: e.InputTokens, OutputTokens: e.OutputTokens,
				CacheRead: e.CacheReadTokens, CacheWrite: e.CacheWriteTokens, Error: e.Error, Attrs: e.Attrs,
			}); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintln(w, traceLine(e)); err != nil {
			return err
		}
	}
	return nil
}

// writePlanRow renders one plan entry; an entry that does not decode is skipped.
func writePlanRow(w io.Writer, enc *json.Encoder, e store.Entry, asJSON bool) error {
	var plan native.Plan
	if err := json.Unmarshal(e.JSON, &plan); err != nil {
		return nil
	}
	if asJSON {
		steps := make([]planStep, len(plan.Steps))
		for i, st := range plan.Steps {
			steps[i] = planStep{Step: st.Step, Status: st.Status}
		}
		return enc.Encode(tracePlan{
			Time: e.CreatedAt.UTC().Format(time.RFC3339Nano), Kind: "plan", TurnID: e.TurnID,
			Explanation: plan.Explanation, Plan: steps,
		})
	}
	at := e.CreatedAt.Local().Format("2006-01-02T15:04:05.000")
	for _, st := range plan.Steps {
		line := fmt.Sprintf("%s plan %s %s", at, planSymbol(st.Status), st.Step)
		if e.TurnID != "" {
			line += " turn=" + e.TurnID
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

// traceLine renders: time kind name status duration [ttft] [tokens] [error] turn.
func traceLine(e store.Event) string {
	if e.Kind == store.EventTurnContext {
		return turnContextLine(e)
	}
	parts := []string{
		e.StartedAt.Local().Format("2006-01-02T15:04:05.000"), e.Kind, e.Name, e.Status,
		fmt.Sprintf("%dms", e.DurationMs),
	}
	if e.TTFTMs > 0 {
		parts = append(parts, fmt.Sprintf("ttft=%dms", e.TTFTMs))
	}
	if e.InputTokens > 0 || e.OutputTokens > 0 {
		parts = append(parts, fmt.Sprintf("in=%d out=%d", e.InputTokens, e.OutputTokens))
		if e.CacheReadTokens > 0 {
			parts = append(parts, fmt.Sprintf("cached=%d", e.CacheReadTokens))
		}
	}
	// A request that broke the previous one's prefix: why, and how much of
	// the earlier content (JSON chars) had to be sent again.
	if reason, _ := e.Attrs["prefix"].(string); reason != "" && reason != prefixAppend {
		parts = append(parts, fmt.Sprintf("prefix=%s(rebuilt=%s)", reason, human(int64(attrInt(e.Attrs["prefix_rebuilt"])))))
	}
	if n, ok := e.Attrs["attempt"]; ok {
		parts = append(parts, fmt.Sprintf("attempt=%v", n))
	}
	if n, ok := e.Attrs["tool_calls"]; ok && fmt.Sprint(n) != "0" {
		parts = append(parts, fmt.Sprintf("calls=%v", n))
	}
	if f, _ := e.Attrs["finish"].(string); f == "truncated" {
		parts = append(parts, "truncated")
	}
	if n := attrInt(e.Attrs["retries"]); n > 0 {
		parts = append(parts, fmt.Sprintf("retries=%d(%s, waited %.1fs)", n, e.Attrs["retry_reason"], float64(attrInt(e.Attrs["retry_wait_ms"]))/1000))
	}
	if e.Kind == store.EventTool {
		parts = append(parts, toolTraceParts(e)...)
	}
	if e.Kind == store.EventContext {
		parts = append(parts, contextTraceParts(e))
	}
	if e.Kind == store.EventHook {
		if d, _ := e.Attrs["decision"].(string); d != "" {
			parts = append(parts, "decision="+d)
		}
		if n := attrInt(e.Attrs["additional_chars"]); n > 0 {
			parts = append(parts, fmt.Sprintf("context+=%d", n))
		}
		if n := attrInt(e.Attrs["reason_chars"]); n > 0 {
			parts = append(parts, fmt.Sprintf("reason=%d", n))
		}
	}
	if e.Kind == store.EventCompaction && e.Name == "compact" {
		parts = append(parts, segmentTraceParts(e)...)
	}
	if e.Kind == store.EventFold {
		// boundary = folded steps of steps; view chars before -> after folding.
		parts = append(parts, fmt.Sprintf("folded=%v/%v chars=%v->%v",
			e.Attrs["boundary"], e.Attrs["steps"], e.Attrs["chars_before"], e.Attrs["chars_after"]))
	}
	if e.Error != "" {
		msg := strings.Join(strings.Fields(e.Error), " ")
		if r := []rune(msg); len(r) > 120 {
			msg = string(r[:120]) + "..."
		}
		parts = append(parts, "err="+msg)
	}
	if e.TurnID != "" {
		parts = append(parts, "turn="+e.TurnID)
	}
	return strings.Join(parts, " ")
}

// turnContextLine renders a turn_context event briefly: time turn_context
// model mode tools tainted rules/memory hash prefixes turn.
func turnContextLine(e store.Event) string {
	attr := func(key string) string {
		if v, ok := e.Attrs[key]; ok {
			return fmt.Sprint(v)
		}
		return ""
	}
	short := func(hash string) string {
		if hash == "" {
			return "-"
		}
		return hash[:min(len(hash), 8)]
	}
	parts := []string{
		e.StartedAt.Local().Format("2006-01-02T15:04:05.000"), e.Kind,
		"model=" + attr("provider") + "/" + attr("model"), "mode=" + attr("mode"),
		"tools=" + attr("tools_count") + "#" + short(attr("tools")), "tainted=" + attr("tainted"),
		"rules=" + short(attr("rules_hash")), "memory=" + short(attr("memory_hash")),
	}
	if v := attr("version"); v != "" {
		parts = append(parts, "suag="+v)
	}
	if e.TurnID != "" {
		parts = append(parts, "turn="+e.TurnID)
	}
	return strings.Join(parts, " ")
}

// toolTraceParts renders what a tool call did: exit code (shell), the size
// that entered the context, a spill marker and the argument summary last.
func toolTraceParts(e store.Event) []string {
	var parts []string
	// Only a person's decision is shown; rule approvals are the norm.
	if src, _ := e.Attrs["approval"].(string); src != "" && src != "rule" {
		item := "approval=" + src
		if ms := attrInt(e.Attrs["approval_wait_ms"]); ms > 0 {
			item += fmt.Sprintf("(wait=%.1fs)", float64(ms)/1000)
		}
		parts = append(parts, item)
	}
	if d, _ := e.Attrs["danger"].(string); d != "" {
		parts = append(parts, fmt.Sprintf("danger=%q", d))
	}
	if code, ok := e.Attrs["exit_code"]; ok {
		parts = append(parts, fmt.Sprintf("exit=%v", code))
	}
	if _, ok := e.Attrs["lines_added"]; ok {
		parts = append(parts, fmt.Sprintf("+%d/-%d", attrInt(e.Attrs["lines_added"]), attrInt(e.Attrs["lines_removed"])))
	}
	if n, ok := e.Attrs["inline_bytes"]; ok {
		parts = append(parts, fmt.Sprintf("bytes=%v", n))
	}
	if spilled, _ := e.Attrs["spilled"].(bool); spilled {
		parts = append(parts, fmt.Sprintf("spilled(raw=%v)", e.Attrs["raw_bytes"]))
	}
	if minimized, _ := e.Attrs["minimized"].(bool); minimized {
		parts = append(parts, fmt.Sprintf("tidied(raw=%v)", e.Attrs["raw_bytes"]))
	}
	if arg, ok := e.Attrs["arg"].(string); ok && arg != "" {
		parts = append(parts, "["+arg+"]")
	}
	return parts
}

// segmentTraceParts renders a segment break (compaction "compact" event):
// segment number (the ordinal of its segment record), reason and the token
// estimate before -> after; missing attributes are left out (older events).
func segmentTraceParts(e store.Event) []string {
	var parts []string
	if n, ok := e.Attrs["segment"]; ok && fmt.Sprint(n) != "0" {
		parts = append(parts, fmt.Sprintf("segment=#%v", n))
	}
	if reason, ok := e.Attrs["reason"]; ok {
		parts = append(parts, fmt.Sprintf("reason=%v", reason))
	}
	before, okB := e.Attrs["tokens_before"]
	after, okA := e.Attrs["tokens_after"]
	switch {
	case okB && okA:
		parts = append(parts, fmt.Sprintf("tokens=%v->%v", before, after))
	case okB:
		parts = append(parts, fmt.Sprintf("tokens_before=%v", before))
	}
	return parts
}

// contextParts are the parts of a context event in the order they are sent.
var contextParts = []string{"system", "tools", "environment", "rules", "memory_index", "skills",
	"append_prompt", "deferred_tools", "handoff", "history", "hook_context", "input"}

// contextTraceParts renders a context event: the chars of each non-empty part
// (tools with their count), then the estimated tokens of the window.
func contextTraceParts(e store.Event) string {
	var parts []string
	for _, key := range contextParts {
		n := attrInt(e.Attrs[key])
		if n == 0 {
			continue
		}
		item := key + "=" + human(int64(n))
		if key == "tools" {
			item += fmt.Sprintf("(%d", attrInt(e.Attrs["tools_count"]))
			if mcp := attrInt(e.Attrs["tools_mcp"]); mcp > 0 {
				item += fmt.Sprintf(", %d mcp", mcp)
			}
			item += ")"
		}
		parts = append(parts, item)
	}
	out := "chars " + strings.Join(parts, " ")
	if est, window := attrInt(e.Attrs["est_tokens"]), attrInt(e.Attrs["window"]); est > 0 && window > 0 {
		out += fmt.Sprintf(" est=%s/%s tokens", human(int64(est)), human(int64(window)))
	}
	return out
}
