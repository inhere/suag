package tasks

import (
	"fmt"
	"strings"
	"time"
)

// Describe is a one-line account of a task, shared by the task tool, the
// end notices and the trace: "t3 exited (exit 0) after 4m12s · 1.2 MB / 3400
// lines · go test ./... · out:sess:ab12"; a function task adds its error and
// progress ("t4 running for 3m · run 5, exit 0 · ...").
func Describe(i Info) string {
	state := string(i.Status)
	switch i.Status {
	case Running:
		state = "running for " + roundDuration(i.Duration())
	case Exited:
		state = fmt.Sprintf("exited (exit %d) after %s", i.ExitCode, roundDuration(i.Duration()))
	default:
		state += " after " + roundDuration(i.Duration())
	}
	size := fmt.Sprintf("%s / %d lines", humanBytes(i.Bytes), i.Lines)
	if i.Truncated {
		size += " (output cut)"
	}
	parts := []string{i.ID + " " + state}
	if i.Error != "" {
		parts[0] += ": " + i.Error
	}
	if i.Progress != "" {
		parts = append(parts, i.Progress)
	}
	parts = append(parts, size)
	if i.Summary != "" {
		parts = append(parts, i.Summary)
	}
	if i.Ref != "" {
		parts = append(parts, i.Ref)
	}
	return strings.Join(parts, " · ")
}

// TailText is the last n lines of the task's tail, one per line.
func TailText(i Info, n int) string {
	tail := i.Tail
	if len(tail) > n {
		tail = tail[len(tail)-n:]
	}
	return strings.Join(tail, "\n")
}

func roundDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return d.Round(time.Millisecond).String()
	case d < time.Minute:
		return d.Round(100 * time.Millisecond).String()
	default:
		return d.Round(time.Second).String()
	}
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
