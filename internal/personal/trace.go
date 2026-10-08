package personal

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/inhere/suag/internal/personal/store"
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

// WriteTrace prints events (oldest first) as a timeline: text mode is one line
// per event, asJSON writes one JSON object per line.
func WriteTrace(w io.Writer, events []store.Event, asJSON bool) error {
	enc := json.NewEncoder(w)
	for _, e := range events {
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
