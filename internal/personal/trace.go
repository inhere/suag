package personal

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/internal/platform/tools/native"
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
	parts := []string{
		e.StartedAt.Local().Format("2006-01-02T15:04:05.000"), e.Kind, e.Name, e.Status,
		fmt.Sprintf("%dms", e.DurationMs),
	}
	if e.TTFTMs > 0 {
		parts = append(parts, fmt.Sprintf("ttft=%dms", e.TTFTMs))
	}
	if e.InputTokens > 0 || e.OutputTokens > 0 {
		parts = append(parts, fmt.Sprintf("in=%d out=%d", e.InputTokens, e.OutputTokens))
	}
	if n, ok := e.Attrs["attempt"]; ok {
		parts = append(parts, fmt.Sprintf("attempt=%v", n))
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
