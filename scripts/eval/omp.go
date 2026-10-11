package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// runOmp runs one task with oh-my-pi (`omp -p --mode json`) in the prepared
// workspace and fills r from its JSON event stream. omp keeps its own auth and
// settings (read only here); --no-session keeps it from writing sessions.
//
// Unlike `suag run`, omp has no read-only mode switch, so read-only tasks run
// with omp's full tool set; the comparison notes this.
func runOmp(ctx context.Context, o *options, t *Task, ws string, r *Result) {
	args := []string{"-p", "--mode", "json", "--auto-approve", "--no-session", "--no-title",
		"--cwd", ws, "--max-time", fmt.Sprintf("%ds", int(o.timeout.Seconds()))}
	if o.model != "" {
		args = append(args, "--model", o.model)
	}
	args = append(args, t.Prompt)

	cmd := exec.CommandContext(ctx, o.ompBin, args...)
	cmd.Dir = ws
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	start := time.Now()
	err := cmd.Run()
	r.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		tail := strings.TrimSpace(stderr.String())
		if len(tail) > 500 {
			tail = tail[len(tail)-500:]
		}
		r.Error = fmt.Sprintf("omp: %v: %s", err, tail)
	}
	parseOmpEvents(stdout.Bytes(), r)
	r.Checks = runChecks(ws, r.Answer, t.Checks)
	r.Passed = err == nil && allPassed(r.Checks)
}

// ompMessage is the subset of an omp assistant message the eval reads.
type ompMessage struct {
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage *struct {
		Input      int64 `json:"input"` // excludes cache reads/writes
		Output     int64 `json:"output"`
		CacheRead  int64 `json:"cacheRead"`
		CacheWrite int64 `json:"cacheWrite"`
		Cost       struct {
			Total float64 `json:"total"`
		} `json:"cost"`
	} `json:"usage"`
}

// parseOmpEvents sums usage over every assistant message_end (one per model
// call), counts tool calls in their content and keeps the text of the last
// assistant message as the answer.
func parseOmpEvents(raw []byte, r *Result) {
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 1<<20), 256<<20) // message_update lines can be large
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev struct {
			Type    string     `json:"type"`
			Message ompMessage `json:"message"`
		}
		if json.Unmarshal(line, &ev) != nil || ev.Type != "message_end" || ev.Message.Role != "assistant" {
			continue
		}
		m := ev.Message
		if m.Usage != nil {
			r.Calls++
			r.InputTokens += m.Usage.Input
			r.OutputToks += m.Usage.Output
			r.CacheRead += m.Usage.CacheRead
			r.CacheWrite += m.Usage.CacheWrite
			r.CostUSD += m.Usage.Cost.Total
		}
		var text []string
		for _, c := range m.Content {
			switch {
			case strings.Contains(strings.ToLower(c.Type), "tool"):
				r.ToolCalls++
			case c.Type == "text" && strings.TrimSpace(c.Text) != "":
				text = append(text, c.Text)
			}
		}
		if len(text) > 0 {
			r.Answer = strings.Join(text, "\n")
		}
	}
	if total := r.InputTokens + r.CacheRead + r.CacheWrite; total > 0 {
		r.CacheHit = float64(r.CacheRead) / float64(total)
	}
}
