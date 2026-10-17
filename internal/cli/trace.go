package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"

	"github.com/gookit/goutil/cflag/capp"
	appconfig "github.com/inhere/suga/internal/config"
	"github.com/inhere/suga/internal/personal"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/internal/wiring"
)

func newTraceCommand(ctx context.Context, stdout, stderr io.Writer) *capp.Cmd {
	var traceTurn, traceAddr string
	var traceJSON, traceAnalyze, traceWeb bool
	trace := capp.NewCmd("trace", "Show the turn/model/tool/denial timeline of a session", func(c *capp.Cmd) error {
		sessionID := c.Arg("session").String()
		if traceWeb {
			return runTraceWeb(ctx, stdout, sessionID, traceAddr)
		}
		return runTrace(ctx, stdout, sessionID, traceTurn, traceJSON, traceAnalyze)
	})
	trace.SetOutput(stderr)
	trace.StringVar(&traceTurn, "turn", "", "only show events of this turn ID")
	trace.BoolVar(&traceJSON, "json", false, "write one JSON object per line")
	trace.BoolVar(&traceWeb, "web", false, "serve the session timeline as a local web page (Ctrl-C to stop)")
	trace.StringVar(&traceAddr, "addr", "127.0.0.1:0", "loopback address for --web (port 0 picks a free one)")
	trace.BoolVar(&traceAnalyze, "analyze", false, "summarize the loop: steps, parallel calls, re-reads, repeated commands, tests after the first pass, background tasks and sub-agents")
	// Flags may also follow the session ID (capp parses them interspersed).
	trace.AddArg("session", "the session ID", true)
	return trace
}

// runTrace prints the event timeline of one session from the core store.
func runTrace(ctx context.Context, out io.Writer, sessionID, turnID string, asJSON, analyze bool) error {
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
	if analyze {
		return personal.WriteTraceAnalysis(out, personal.AnalyzeTrace(events), asJSON)
	}
	// Plan entries come from the entries table and are merged by creation time.
	plans, err := repo.ListEntries(ctx, sessionID, store.EntryFilter{Types: []string{store.EntryPlan}, TurnID: turnID})
	if err != nil {
		return err
	}
	return personal.WriteTrace(out, events, plans, asJSON)
}

// runTraceWeb serves the session timeline page until ctx ends (Ctrl-C).
func runTraceWeb(ctx context.Context, out io.Writer, sessionID, addr string) error {
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	dbPath, err := appconfig.ResolveCoreStorePath()
	if err != nil {
		return err
	}
	// A read-only page must not create or migrate the store: an uninitialized
	// or missing session store is an error, not something to write.
	repo, err := store.OpenReadOnly(dbPath)
	if err != nil {
		return fmt.Errorf("trace --web: %w", err)
	}
	defer repo.Close()
	view, err := personal.BuildTraceView(ctx, repo, sessionID)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	// Loaded per request: the page refreshes (and polls /api/session with its
	// own since_seq / since_event while the session runs), so it shows new
	// steps without re-sending the old ones.
	load := func(ctx context.Context, q personal.TraceQuery) (*personal.TraceView, error) {
		return personal.BuildTraceViewSince(ctx, repo, sessionID, q)
	}
	// The environment a session in its directory would get now; read per
	// request, so an edited config shows on reload.
	env := func(context.Context) (any, error) { return wiring.LoadLocalEnv(view.Session.Cwd) }
	return personal.ServeTraceView(ctx, addr, load, env, func(url string) {
		fmt.Fprintf(out, "trace of %s: %s (Ctrl-C to stop)\n", sessionID, url)
		openBrowser(url)
	})
}

// openBrowser opens url in the default browser, best-effort.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return // no desktop: the printed URL is enough
		}
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
