package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gookit/goutil/cflag/capp"
	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/personal/store"
)

// newStatsCommand builds `suag stats`.
func newStatsCommand(ctx context.Context, stdout, stderr io.Writer) *capp.Cmd {
	var since, session string
	var asJSON bool
	cmd := capp.NewCmd("stats", "Summarize model/tool calls, latency, tokens and cache hits", func(c *capp.Cmd) error {
		if len(c.RemainArgs()) > 0 {
			return fmt.Errorf("usage: suag stats [--since 7d] [--session id] [--json]")
		}
		return runStats(ctx, stdout, since, session, asJSON)
	})
	cmd.SetOutput(stderr)
	cmd.StringVar(&since, "since", "", "only events newer than this, e.g. 12h or 7d")
	cmd.StringVar(&session, "session", "", "only events of this session ID;;sid")
	cmd.BoolVar(&asJSON, "json", false, "write a single JSON object")
	return cmd
}

// newExportCommand builds `suag export`.
func newExportCommand(ctx context.Context, stdout, stderr io.Writer) *capp.Cmd {
	var redact bool
	var outFile string
	cmd := capp.NewCmd("export", "Export a session's entries and events as jsonl", func(c *capp.Cmd) error {
		// Flags may follow the session ID, which the flag parser leaves in the remaining args.
		var positional []string
		rest := c.RemainArgs()
		for i := 0; i < len(rest); i++ {
			switch {
			case rest[i] == "--redact" || rest[i] == "-redact":
				redact = true
			case (rest[i] == "-o" || rest[i] == "--o" || rest[i] == "--output") && i+1 < len(rest):
				i++
				outFile = rest[i]
			default:
				positional = append(positional, rest[i])
			}
		}
		if len(positional) != 1 {
			return fmt.Errorf("usage: suag export <session> [--redact] [-o file]")
		}
		return runExport(ctx, stdout, positional[0], redact, outFile)
	})
	cmd.SetOutput(stderr)
	cmd.BoolVar(&redact, "redact", false, "drop tool call arguments")
	cmd.StringVar(&outFile, "output", "", "write to this file (mode 0600) instead of stdout;;o")
	return cmd
}

func openCoreStore() (*store.Store, error) {
	dbPath, err := appconfig.ResolveCoreStorePath()
	if err != nil {
		return nil, err
	}
	return store.Open(dbPath)
}

// parseSince parses "<n>h" or "<n>d" into a duration.
func parseSince(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid --since %q: want <n>h or <n>d", s)
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid --since %q: want <n>h or <n>d", s)
	}
	switch s[len(s)-1] {
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return 0, fmt.Errorf("invalid --since %q: want <n>h or <n>d", s)
}

func runStats(ctx context.Context, out io.Writer, since, session string, asJSON bool) error {
	f := store.EventFilter{SessionID: session}
	if since != "" {
		d, err := parseSince(since)
		if err != nil {
			return err
		}
		f.Since = time.Now().Add(-d)
	}
	repo, err := openCoreStore()
	if err != nil {
		return err
	}
	defer repo.Close()
	if session != "" {
		if _, err := repo.GetSession(ctx, session); err != nil {
			return fmt.Errorf("stats session %q: %w", session, err)
		}
	}
	rep, err := repo.BuildReport(ctx, f)
	if err != nil {
		return err
	}
	if asJSON {
		return json.NewEncoder(out).Encode(rep)
	}
	return writeStatsText(out, rep)
}

func writeStatsText(out io.Writer, rep store.Report) error {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "MODEL\tCALLS\tERR%\tP50\tP95\tTTFT-P50\tIN\tOUT\tCACHE-R\tCACHE-W\tHIT%\tCOST")
	for _, m := range rep.Models {
		ttft := "-"
		if m.P50TTFTMs > 0 {
			ttft = fmt.Sprintf("%dms", m.P50TTFTMs)
		}
		fmt.Fprintf(tw, "%s/%s\t%d\t%.1f\t%dms\t%dms\t%s\t%d\t%d\t%d\t%d\t%.1f\t$%.4f\n",
			m.Provider, m.Model, m.Calls, m.ErrorRate*100, m.P50Ms, m.P95Ms, ttft,
			m.InputTokens, m.OutputTokens, m.CacheRead, m.CacheWrite, m.CacheHitRate*100, m.CostUSD)
	}
	fmt.Fprintln(tw, "\t\t\t\t\t\t\t\t\t\t\t")
	fmt.Fprintln(tw, "TOOL\tCALLS\tERR%\tDENIED\tP50\tP95\t\t\t\t\t\t")
	for _, t := range rep.Tools {
		fmt.Fprintf(tw, "%s\t%d\t%.1f\t%d\t%dms\t%dms\t\t\t\t\t\t\n", t.Name, t.Calls, t.ErrorRate*100, t.Denials, t.P50Ms, t.P95Ms)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(out, "\ncompactions: %d  tool_searches: %d\n", rep.Compactions, rep.ToolSearch)
	return err
}

// runExport writes a session's entries and events (merged by time) as jsonl.
func runExport(ctx context.Context, stdout io.Writer, sessionID string, redact bool, outFile string) error {
	repo, err := openCoreStore()
	if err != nil {
		return err
	}
	defer repo.Close()
	if _, err := repo.GetSession(ctx, sessionID); err != nil {
		return fmt.Errorf("export session %q: %w", sessionID, err)
	}
	entries, err := repo.ListEntries(ctx, sessionID, store.EntryFilter{})
	if err != nil {
		return err
	}
	events, err := repo.ListEvents(ctx, store.EventFilter{SessionID: sessionID})
	if err != nil {
		return err
	}
	var buf strings.Builder
	if err := writeExport(&buf, entries, events, redact); err != nil {
		return err
	}
	if outFile == "" {
		_, err = io.WriteString(stdout, buf.String())
		return err
	}
	return os.WriteFile(outFile, []byte(buf.String()), 0o600)
}

// secretArgKeys are event attr keys that may hold tool-call arguments.
var secretArgKeys = []string{"arguments", "args", "input", "params", "parameters"}

// writeExport merges entries and events by time (entries first on ties, and
// entries keep seq order, events keep id order) and writes one JSON object per
// line.
func writeExport(w io.Writer, entries []store.Entry, events []store.Event, redact bool) error {
	enc := json.NewEncoder(w)
	i, j := 0, 0
	for i < len(entries) || j < len(events) {
		if j >= len(events) || (i < len(entries) && !entries[i].CreatedAt.After(events[j].StartedAt)) {
			e := entries[i]
			i++
			body := e.JSON
			if redact {
				body = redactEntryJSON(body)
			}
			if err := enc.Encode(map[string]any{
				"kind": "entry", "session_id": e.SessionID, "seq": e.Seq, "turn_id": e.TurnID,
				"type": e.Type, "created_at": e.CreatedAt.UTC().Format(time.RFC3339Nano), "json": json.RawMessage(body),
			}); err != nil {
				return err
			}
			continue
		}
		e := events[j]
		j++
		attrs := e.Attrs
		if redact {
			attrs = make(map[string]any, len(e.Attrs))
			for k, v := range e.Attrs {
				attrs[k] = v
			}
			for _, k := range secretArgKeys {
				delete(attrs, k)
			}
		}
		if err := enc.Encode(map[string]any{
			"kind": "event", "id": e.ID, "session_id": e.SessionID, "turn_id": e.TurnID,
			"event_kind": e.Kind, "name": e.Name, "status": e.Status,
			"started_at":  e.StartedAt.UTC().Format(time.RFC3339Nano),
			"duration_ms": e.DurationMs, "ttft_ms": e.TTFTMs,
			"input_tokens": e.InputTokens, "output_tokens": e.OutputTokens,
			"cache_read_tokens": e.CacheReadTokens, "cache_write_tokens": e.CacheWriteTokens,
			"error": e.Error, "attrs": attrs,
		}); err != nil {
			return err
		}
	}
	return nil
}

// redactEntryJSON removes ToolCall.Arguments from a serialized agentcore
// Message (ToolCall has no json tags, so the key is "Arguments"; a lowercase
// spelling is handled too). Unparseable bodies are returned as-is.
func redactEntryJSON(raw json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	for _, key := range []string{"ToolCall", "tool_call"} {
		var tc map[string]json.RawMessage
		if json.Unmarshal(m[key], &tc) != nil || tc == nil {
			continue
		}
		delete(tc, "Arguments")
		delete(tc, "arguments")
		b, err := json.Marshal(tc)
		if err != nil {
			return raw
		}
		m[key] = b
	}
	b, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return b
}
