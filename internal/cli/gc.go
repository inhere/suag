package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gookit/goutil/cflag/capp"
	appconfig "github.com/inhere/suga/internal/config"
	personalprocess "github.com/inhere/suga/internal/personal/process"
	"github.com/inhere/suga/internal/personal/store"
)

// newGCCommand builds `suga gc`: remove sessions idle for longer than
// --older-than. It only reports by default; --apply deletes.
func newGCCommand(ctx context.Context, stdout, stderr io.Writer) *capp.Cmd {
	var olderThan string
	var apply bool
	cmd := capp.NewCmd("gc", "Remove sessions (and their events) not updated within --older-than; dry run unless --apply", func(c *capp.Cmd) error {
		if len(c.RemainArgs()) > 0 || olderThan == "" {
			return fmt.Errorf("usage: suga gc --older-than 30d [--apply]")
		}
		age, err := parseSince(olderThan)
		if err != nil {
			return fmt.Errorf("invalid --older-than %q: want <n>h or <n>d", olderThan)
		}
		return runGC(ctx, stdout, time.Now().Add(-age), apply)
	})
	cmd.SetOutput(stderr)
	cmd.StringVar(&olderThan, "older-than", "", "remove sessions not updated within this age, e.g. 30d or 12h")
	cmd.BoolVar(&apply, "apply", false, "delete for real (default: only list what would be removed)")
	return cmd
}

func runGC(ctx context.Context, out io.Writer, cutoff time.Time, apply bool) error {
	home, err := appconfig.ResolveSugaHome()
	if err != nil {
		return err
	}
	repo, err := openCoreStore()
	if err != nil {
		return err
	}
	defer repo.Close()

	// Never touch a session a live process uses: open chats are listed under
	// run/, and a running turn (chat or `run`) holds run/locks/<id>.lock.
	keep := map[string]bool{}
	records, err := personalprocess.List(filepath.Join(home, "run"))
	if err != nil {
		return err
	}
	for _, record := range records {
		keep[record.SessionID] = true
	}
	plan, err := repo.CollectGarbage(ctx, cutoff, keep, false)
	if err != nil {
		return err
	}
	lockRoot := filepath.Join(home, "run", "locks")
	var held []*personalprocess.SessionLock
	defer func() {
		for _, lock := range held {
			_ = lock.Close()
		}
	}()
	for _, session := range plan.Sessions {
		lock, err := personalprocess.AcquireSessionLock(lockRoot, session.ID, 0)
		if err != nil {
			keep[session.ID] = true // busy: some process is using it right now
			continue
		}
		held = append(held, lock) // held until the deletion is done
	}

	report, err := repo.CollectGarbage(ctx, cutoff, keep, apply)
	if err != nil {
		return err
	}
	if report.Applied {
		// The readable per-session transcript (transcripts/<id>.jsonl) is not
		// in the database; remove it with its session whatever the current
		// transcripts setting, since an earlier run may have written it.
		// Likewise the session directory (sessions/<id>/, externalised tool
		// outputs) lives outside the database and goes with its session.
		sessionsDir, err := appconfig.ResolveSessionsDir()
		if err != nil {
			return err
		}
		for _, session := range report.Sessions {
			if err := os.Remove(filepath.Join(home, "transcripts", session.ID+".jsonl")); err != nil && !errors.Is(err, os.ErrNotExist) {
				fmt.Fprintf(out, "warning: remove transcript of %s: %v\n", session.ID, err)
			}
			if dir, ok := sessionDir(sessionsDir, session.ID); ok {
				if err := os.RemoveAll(dir); err != nil {
					fmt.Fprintf(out, "warning: remove session directory of %s: %v\n", session.ID, err)
				}
			}
		}
	}
	writeGCReport(out, report, cutoff)
	return nil
}

// sessionDir returns sessions/<id> when id is a plain single path element; an
// id that could name another place (separators, "..", absolute) yields false,
// so a stored id can never make gc remove anything outside the sessions root.
func sessionDir(sessionsDir, id string) (string, bool) {
	if id == "" || id == "." || strings.ContainsAny(id, `/\:`) || !filepath.IsLocal(id) {
		return "", false
	}
	return filepath.Join(sessionsDir, id), true
}

func writeGCReport(out io.Writer, report store.GCReport, cutoff time.Time) {
	for _, session := range report.Sessions {
		title := session.Title
		if title == "" {
			title = "-"
		}
		fmt.Fprintf(out, "%s\t%s\t%s\n", session.ID, session.UpdatedAt.Local().Format("2006-01-02 15:04"), title)
	}
	verb := "将删除"
	if report.Applied {
		verb = "已删除"
	}
	fmt.Fprintf(out, "%s %d 个会话（%s 之前未更新，含其 transcript 文件与会话目录）与 %d 条事件记录", verb, len(report.Sessions),
		cutoff.Local().Format("2006-01-02 15:04"), report.Events)
	if !report.Applied {
		fmt.Fprint(out, "；加 --apply 执行")
	}
	fmt.Fprintln(out)
}
