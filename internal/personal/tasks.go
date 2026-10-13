package personal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/tasks"
)

// TaskNotices hands over the session's ended background tasks the model has
// not seen yet (tasks.Manager).
type TaskNotices interface {
	TakeNotices(sessionID string) []tasks.Info
}

// metaTaskNotice marks the user message carrying background task notices.
const metaTaskNotice = "suag.task_notice"

// noticeTailLines are shown per task in a notice.
const noticeTailLines = 5

// TaskNoticeText is the text of a notice for ended tasks and for what
// running tasks reported (a watch whose result changed).
func TaskNoticeText(infos []tasks.Info) string {
	var b strings.Builder
	head := "These background tasks ended"
	for _, info := range infos {
		if info.Notice != "" {
			head = "Background task updates"
			break
		}
	}
	b.WriteString("[background task notice] " + head + " (read more with the task tool or recall the ref):")
	for _, info := range infos {
		if info.Notice != "" { // the report of a task (sent while it ran)
			b.WriteString("\n- " + info.ID + " (" + info.Kind + ") reports: " + strings.ReplaceAll(info.Notice, "\n", "\n  "))
			continue
		}
		b.WriteString("\n- " + tasks.Describe(info))
		if tail := tasks.TailText(info, noticeTailLines); tail != "" {
			b.WriteString("\n  last lines:\n  " + strings.ReplaceAll(tail, "\n", "\n  "))
		}
	}
	return b.String()
}

// TaskNoticeMessage is the user message of a notice.
func TaskNoticeMessage(infos []tasks.Info) agentcore.Message {
	return agentcore.Message{Role: "user", Content: TaskNoticeText(infos), Metadata: map[string]any{metaTaskNotice: true}}
}

// turnNotice is a task notice taken during the turn and where it goes: after
// the first at messages of state.Messages.
type turnNotice struct {
	at  int
	msg agentcore.Message
}

// takeTaskNotices moves the session's pending task notices into this turn
// (before the coming model request) and stores them as a user entry, so
// later turns replay them in place.
func (b *contextBuilder) takeTaskNotices(ctx context.Context, state *agentcore.RunState) {
	src := b.r.cfg.Tasks
	if src == nil || state == nil {
		return
	}
	infos := src.TakeNotices(b.sessionID)
	if len(infos) == 0 {
		return
	}
	msg := TaskNoticeMessage(infos)
	b.notices = append(b.notices, turnNotice{at: len(state.Messages), msg: msg})
	raw, err := json.Marshal(msg)
	if err == nil {
		_, err = b.r.cfg.Repository.AppendEntries(context.WithoutCancel(ctx), b.sessionID,
			store.NewEntry{TurnID: b.turnID, Type: store.EntryUser, JSON: raw})
	}
	if err != nil {
		slog.Warn("store task notice", "session_id", b.sessionID, "err", err)
	}
}

// RecordTaskEnd records a background task's end in the trace (a task event
// spanning its whole run). It is called from the task's own goroutine.
func (r *Runner) RecordTaskEnd(info tasks.Info) {
	r.rec.record(context.Background(), taskEndEvent(info))
}

// RecordTaskEndIn records a task end straight into repo, for hosts whose
// tasks outlive the runner (ACP opens a runner per prompt, so a task may end
// between turns).
func RecordTaskEndIn(repo *store.Store, info tasks.Info) {
	(&recorder{sink: repo}).record(context.Background(), taskEndEvent(info))
}

// taskEndEvent is the task event of an ended task.
func taskEndEvent(info tasks.Info) store.Event {
	attrs := map[string]any{"arg": info.Summary, "kind": info.Kind, "exit_code": info.ExitCode,
		"bytes": info.Bytes, "lines": info.Lines, "ref": info.Ref}
	if info.Truncated {
		attrs["truncated"] = true
	}
	// What a function task reported: a watch's runs and changes, its state.
	for name, n := range info.Counters {
		attrs[name] = n
	}
	for name, value := range info.Labels {
		attrs[name] = value
	}
	if info.Progress != "" {
		attrs["progress"] = info.Progress
	}
	status := eventOK
	if info.Status != tasks.Exited || info.ExitCode != 0 {
		status = string(info.Status)
	}
	return store.Event{Kind: store.EventTask, Name: info.ID, SessionID: info.SessionID,
		Status: status, StartedAt: info.Started, DurationMs: info.Duration().Milliseconds(), Attrs: attrs,
		Error: errorOfTask(info)}
}

func errorOfTask(info tasks.Info) string {
	if info.Status == tasks.Exited && info.ExitCode != 0 {
		return fmt.Sprintf("exit code %d", info.ExitCode)
	}
	if info.Status == tasks.TimedOut {
		return "ran past tasks.max_duration " + time.Duration(info.Duration()).Round(time.Second).String()
	}
	return info.Error // a function task that failed
}
