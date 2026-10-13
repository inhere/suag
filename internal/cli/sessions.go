package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/gookit/goutil/cflag/capp"
	"github.com/inhere/suag/internal/personal/store"
)

// sessionsOptions are the filters of `suag sessions`.
type sessionsOptions struct {
	limit        int
	since, until string
	query, cwd   string
	asJSON       bool
}

func newSessionsCommand(ctx context.Context, stdout, stderr io.Writer) *capp.Cmd {
	opts := sessionsOptions{limit: 10}
	cmd := capp.NewCmd("sessions", "List sessions, newest first (default the last 10)", func(c *capp.Cmd) error {
		// Keyword arguments add to --query.
		if words := strings.Join(c.Arg("keywords").Strings(), " "); words != "" {
			opts.query = strings.TrimSpace(opts.query + " " + words)
		}
		return runSessions(ctx, stdout, opts, time.Now())
	})
	cmd.SetOutput(stderr)
	cmd.IntVar(&opts.limit, "limit", 10, "how many sessions to show, 0 shows all;;n")
	cmd.StringVar(&opts.since, "since", "", "only sessions active since: 12h, 7d, or a date like 2026-10-01")
	cmd.StringVar(&opts.until, "until", "", "only sessions last active before: 12h, 7d, or a date")
	cmd.StringVar(&opts.query, "query", "", "keywords (all must match) in the title, id, directory, model or the conversation;;q")
	cmd.StringVar(&opts.cwd, "cwd", "", "only sessions whose directory starts with this path (. = current directory)")
	cmd.BoolVar(&opts.asJSON, "json", false, "write a JSON array")
	cmd.AddArg("keywords", "keywords, like --query", false, nil, true)
	return cmd
}

// sessionRow is one listed session (also the --json form).
type sessionRow struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Cwd         string    `json:"cwd"`
	Status      string    `json:"status"`
	Provider    string    `json:"provider,omitempty"`
	Model       string    `json:"model,omitempty"`
	SuagVersion string    `json:"suag_version,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	// Match is a snippet of the conversation when the keyword matched there.
	Match string `json:"match,omitempty"`
}

func runSessions(ctx context.Context, out io.Writer, opts sessionsOptions, now time.Time) error {
	var from, to time.Time
	var err error
	if opts.since != "" {
		if from, err = parseWhen(opts.since, now); err != nil {
			return fmt.Errorf("--since: %w", err)
		}
	}
	if opts.until != "" {
		if to, err = parseWhen(opts.until, now); err != nil {
			return fmt.Errorf("--until: %w", err)
		}
	}
	cwd := opts.cwd
	if cwd == "." {
		if cwd, err = os.Getwd(); err != nil {
			return err
		}
	}
	repo, err := openCoreStore()
	if err != nil {
		return err
	}
	defer repo.Close()
	all, err := repo.ListSessions(ctx)
	if err != nil {
		return err
	}
	// Keywords match the session's own fields, or its conversation (the
	// ledger index; the first snippet is shown).
	terms := strings.Fields(strings.ToLower(opts.query))
	snippets := map[string]string{}
	if len(terms) > 0 {
		hits, err := repo.SearchEntries(ctx, store.EntrySearch{Query: opts.query, Limit: 100})
		if err != nil {
			return err
		}
		for _, h := range hits {
			if _, seen := snippets[h.SessionID]; !seen {
				snippets[h.SessionID] = h.Snippet
			}
		}
	}
	var rows []sessionRow
	for _, s := range all {
		if !from.IsZero() && s.UpdatedAt.Before(from) || !to.IsZero() && s.UpdatedAt.After(to) {
			continue
		}
		if cwd != "" && !pathHasPrefix(s.Cwd, cwd) {
			continue
		}
		row := sessionRow{ID: s.ID, Title: s.Title, Cwd: s.Cwd, Status: s.Status, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt}
		row.Provider, _ = s.Meta["provider"].(string)
		row.Model, _ = s.Meta["model"].(string)
		row.SuagVersion, _ = s.Meta["suag_version"].(string)
		if len(terms) > 0 {
			own := strings.ToLower(strings.Join([]string{s.ID, s.Title, s.Cwd, row.Provider, row.Model}, " "))
			matched := true
			for _, t := range terms {
				matched = matched && strings.Contains(own, t)
			}
			if snippet, ok := snippets[s.ID]; !matched && ok {
				row.Match, matched = snippet, true
			}
			if !matched {
				continue
			}
		}
		rows = append(rows, row)
	}
	total := len(rows)
	if opts.limit > 0 && len(rows) > opts.limit {
		rows = rows[:opts.limit]
	}
	if opts.asJSON {
		if rows == nil {
			rows = []sessionRow{}
		}
		return json.NewEncoder(out).Encode(rows)
	}
	return writeSessionRows(out, rows, total)
}

func writeSessionRows(out io.Writer, rows []sessionRow, total int) error {
	if len(rows) == 0 {
		_, err := fmt.Fprintln(out, "no sessions")
		return err
	}
	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "UPDATED\tID\tMODEL\tSUAG\tTITLE\tDIRECTORY")
	for _, r := range rows {
		model := r.Model
		if r.Provider != "" {
			model = r.Provider + "/" + r.Model
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", r.UpdatedAt.Local().Format("01-02 15:04"), r.ID,
			orDash(model), orDash(r.SuagVersion), orDash(clip(oneLine(r.Title), 40)), orDash(shortDir(r.Cwd)))
		if r.Match != "" {
			fmt.Fprintf(tw, "\t  ↳ %s\t\t\t\t\n", clip(oneLine(r.Match), 100))
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if total > len(rows) {
		_, err := fmt.Fprintf(out, "%d of %d sessions; -n 0 shows all\n", len(rows), total)
		return err
	}
	return nil
}

// parseWhen reads a point in time: a duration back from now (12h, 7d) or a
// local date / date-time (2026-10-01, 2026-10-01 15:04, RFC 3339).
func parseWhen(s string, now time.Time) (time.Time, error) {
	if d, err := parseSince(s); err == nil {
		return now.Add(-d), nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("%q: want 12h, 7d or a date like 2026-10-01", s)
}

// slashPath writes both separators as "/": sessions recorded on Windows are
// listed on any system.
func slashPath(p string) string { return strings.ReplaceAll(p, "\\", "/") }

// pathHasPrefix compares directories by their slash form, ignoring case
// (Windows paths).
func pathHasPrefix(dir, prefix string) bool {
	norm := func(p string) string { return strings.ToLower(strings.TrimRight(slashPath(p), "/")) }
	d, p := norm(dir), norm(prefix)
	return d == p || strings.HasPrefix(d, p+"/")
}

// shortDir keeps the last two path elements.
func shortDir(p string) string {
	parts := strings.Split(strings.TrimRight(slashPath(p), "/"), "/")
	if len(parts) <= 2 {
		return p
	}
	return "…/" + strings.Join(parts[len(parts)-2:], "/")
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
