package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/gookit/goutil/cflag/capp"
	appconfig "github.com/inhere/suag/internal/config"
	personalprocess "github.com/inhere/suag/internal/personal/process"
	"github.com/inhere/suag/internal/personal/store"
)

// newGCCommand builds `suag gc`: remove sessions idle for longer than
// --older-than. It only reports by default; --apply deletes.
func newGCCommand(ctx context.Context, stdout, stderr io.Writer) *capp.Cmd {
	var olderThan string
	var apply bool
	cmd := capp.NewCmd("gc", "Remove sessions (and their events) not updated within --older-than; dry run unless --apply", func(c *capp.Cmd) error {
		if len(c.RemainArgs()) > 0 || olderThan == "" {
			return fmt.Errorf("usage: suag gc --older-than 30d [--apply]")
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
	home, err := appconfig.ResolveSuagHome()
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
	writeGCReport(out, report, cutoff)
	return nil
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
	fmt.Fprintf(out, "%s %d 个会话（%s 之前未更新）与 %d 条事件记录", verb, len(report.Sessions),
		cutoff.Local().Format("2006-01-02 15:04"), report.Events)
	if !report.Applied {
		fmt.Fprint(out, "；加 --apply 执行")
	}
	fmt.Fprintln(out)
}
