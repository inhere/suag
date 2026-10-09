package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/gookit/goutil/cflag/capp"
	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/internal/personal/store"
)

func newTraceCommand(ctx context.Context, stdout, stderr io.Writer) *capp.Cmd {
	var traceTurn string
	var traceJSON bool
	trace := capp.NewCmd("trace", "Show the turn/model/tool/denial timeline of a session", func(c *capp.Cmd) error {
		// Flags may also follow the session ID, which the flag parser leaves in the remaining args.
		var positional []string
		rest := c.RemainArgs()
		for i := 0; i < len(rest); i++ {
			switch {
			case rest[i] == "--json" || rest[i] == "-json":
				traceJSON = true
			case (rest[i] == "--turn" || rest[i] == "-turn") && i+1 < len(rest):
				i++
				traceTurn = rest[i]
			default:
				positional = append(positional, rest[i])
			}
		}
		if len(positional) != 1 {
			return fmt.Errorf("usage: suag trace <session> [--turn id] [--json]")
		}
		return runTrace(ctx, stdout, positional[0], traceTurn, traceJSON)
	})
	trace.SetOutput(stderr)
	trace.StringVar(&traceTurn, "turn", "", "only show events of this turn ID")
	trace.BoolVar(&traceJSON, "json", false, "write one JSON object per line")
	return trace
}

// runTrace prints the event timeline of one session from the core store.
func runTrace(ctx context.Context, out io.Writer, sessionID, turnID string, asJSON bool) error {
	dbPath, err := appconfig.ResolveCoreStorePath()
	if err != nil {
		return err
	}
	repo, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer repo.Close()
	if _, err := repo.GetSession(ctx, sessionID); err != nil {
		return fmt.Errorf("trace session %q: %w", sessionID, err)
	}
	events, err := repo.ListEvents(ctx, store.EventFilter{SessionID: sessionID, TurnID: turnID})
	if err != nil {
		return err
	}
	// Plan entries come from the entries table and are merged by creation time.
	plans, err := repo.ListEntries(ctx, sessionID, store.EntryFilter{Types: []string{store.EntryPlan}, TurnID: turnID})
	if err != nil {
		return err
	}
	return personal.WriteTrace(out, events, plans, asJSON)
}
