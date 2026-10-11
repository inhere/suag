package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gookit/goutil/cflag/capp"
	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/personal/store"
)

// newExportCommand builds `suag export`.
func newExportCommand(ctx context.Context, stdout, stderr io.Writer) *capp.Cmd {
	var redact, withOutputs bool
	var outFile string
	cmd := capp.NewCmd("export", "Export a session's entries and events as jsonl", func(c *capp.Cmd) error {
		// Flags may follow the session ID, which the flag parser leaves in the remaining args.
		var positional []string
		rest := c.RemainArgs()
		for i := 0; i < len(rest); i++ {
			switch {
			case rest[i] == "--redact" || rest[i] == "-redact":
				redact = true
			case rest[i] == "--with-outputs" || rest[i] == "-with-outputs":
				withOutputs = true
			case (rest[i] == "-o" || rest[i] == "--o" || rest[i] == "--output") && i+1 < len(rest):
				i++
				outFile = rest[i]
			default:
				positional = append(positional, rest[i])
			}
		}
		if len(positional) != 1 {
			return fmt.Errorf("usage: suag export <session> [--redact] [--with-outputs] [-o file]")
		}
		return runExport(ctx, stdout, positional[0], exportOptions{redact: redact, withOutputs: withOutputs, outFile: outFile})
	})
	cmd.SetOutput(stderr)
	cmd.BoolVar(&redact, "redact", false, "drop tool call arguments")
	cmd.BoolVar(&withOutputs, "with-outputs", false, "append the session's externalised tool outputs (one output row per file)")
	cmd.StringVar(&outFile, "output", "", "write to this file (mode 0600) instead of stdout;;o")
	return cmd
}

// exportOptions are the flags of `suag export`.
type exportOptions struct {
	redact      bool
	withOutputs bool
	outFile     string
}

// runExport writes a session's entries and events (merged by time) as jsonl;
// with withOutputs the files under sessions/<id>/outputs/ follow as rows of
// kind "output".
func runExport(ctx context.Context, stdout io.Writer, sessionID string, opts exportOptions) error {
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
	if err := writeExport(&buf, entries, events, opts.redact); err != nil {
		return err
	}
	if opts.withOutputs {
		if err := writeExportOutputs(&buf, sessionID); err != nil {
			return err
		}
	}
	if opts.outFile == "" {
		_, err = io.WriteString(stdout, buf.String())
		return err
	}
	return os.WriteFile(opts.outFile, []byte(buf.String()), 0o600)
}

// writeExportOutputs appends one {"kind":"output","ref",...,"content"} row per
// file in the session's outputs directory, sorted by name. Files are read
// through os.Root so a planted symlink cannot pull in anything outside it; a
// session without outputs adds nothing.
func writeExportOutputs(w io.Writer, sessionID string) error {
	sessionsDir, err := appconfig.ResolveSessionsDir()
	if err != nil {
		return err
	}
	dir, ok := sessionDir(sessionsDir, sessionID)
	if !ok {
		return nil
	}
	root, err := os.OpenRoot(filepath.Join(dir, "outputs"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	names, err := f.Readdirnames(-1)
	_ = f.Close()
	if err != nil {
		return err
	}
	sort.Strings(names)
	enc := json.NewEncoder(w)
	for _, name := range names {
		stem, isText := strings.CutSuffix(name, ".txt")
		if !isText {
			continue
		}
		body, err := root.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read output %s: %w", name, err)
		}
		if err := enc.Encode(map[string]any{
			"kind": "output", "session_id": sessionID, "ref": "out:" + sessionID + ":" + stem,
			"bytes": len(body), "content": string(body),
		}); err != nil {
			return err
		}
	}
	return nil
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
