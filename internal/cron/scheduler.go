package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/kagent/kagent/internal/llm"
	"github.com/kagent/kagent/internal/orchestrator"
	"github.com/kagent/kagent/internal/tools"
)

// Scheduler runs agentic cron jobs on their configured schedules.
type Scheduler struct {
	mu       sync.Mutex
	jobs     map[string]*Job
	orch     *orchestrator.Orchestrator
	llm      llm.Provider
	registry *tools.Registry
	stop     chan struct{}
}

// NewScheduler creates a Scheduler.
func NewScheduler(orch *orchestrator.Orchestrator, provider llm.Provider, registry *tools.Registry) *Scheduler {
	return &Scheduler{
		jobs:     make(map[string]*Job),
		orch:     orch,
		llm:      provider,
		registry: registry,
		stop:     make(chan struct{}),
	}
}

// AddJob registers a cron job.
func (s *Scheduler) AddJob(job *Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = job
}

// Start begins the scheduling loop. Runs until ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			s.tick(ctx, t)
		}
	}
}

// tick checks all jobs and fires those whose schedule matches now.
func (s *Scheduler) tick(ctx context.Context, now time.Time) {
	s.mu.Lock()
	jobs := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		if j.Enabled {
			jobs = append(jobs, j)
		}
	}
	s.mu.Unlock()

	for _, job := range jobs {
		if matchCron(job.Schedule, now) {
			go s.runJob(ctx, job)
		}
	}
}

// runJob executes a single agentic cron job.
func (s *Scheduler) runJob(ctx context.Context, job *Job) {
	slog.Info("cron job starting", "job", job.Name)
	job.LastRunAt = time.Now()

	// ── Sense phase: collect data via tools ──────────────────────────────────
	senseData := s.sense(ctx, job)

	// ── Decision phase: ask LLM if action is needed ──────────────────────────
	prompt := strings.ReplaceAll(job.DecisionPrompt, "{{data}}", senseData)
	if prompt == "" {
		prompt = fmt.Sprintf("Based on the following data, should I take action? Respond with JSON {\"action\": bool, \"reason\": string}.\n\nData:\n%s", senseData)
	}

	decResp, err := s.llm.Complete(ctx, &llm.CompletionRequest{
		Messages:    []llm.Message{{Role: "user", Content: prompt}},
		MaxTokens:   256,
		Temperature: 0.1,
	})
	if err != nil {
		slog.Error("cron decision error", "job", job.Name, "err", err)
		job.LastResult = "decision error: " + err.Error()
		return
	}

	// Parse decision JSON.
	var decision struct {
		Action bool   `json:"action"`
		Reason string `json:"reason"`
	}
	// Extract JSON from the LLM response (may have surrounding text).
	raw := extractJSON(decResp.Content)
	if err := json.Unmarshal([]byte(raw), &decision); err != nil {
		slog.Warn("cron decision parse error", "job", job.Name, "raw", decResp.Content)
		job.LastResult = "status normal (parse error)"
		return
	}

	if !decision.Action {
		slog.Info("cron: no action needed", "job", job.Name, "reason", decision.Reason)
		job.LastResult = "status normal: " + decision.Reason
		return
	}

	// ── Execution phase: run a full ReAct task ───────────────────────────────
	slog.Info("cron: taking action", "job", job.Name, "reason", decision.Reason)
	taskDesc := fmt.Sprintf("[Cron job: %s]\nReason: %s\n\nContext data:\n%s", job.Name, decision.Reason, senseData)
	result, err := s.orch.RunTask(ctx, taskDesc)
	if err != nil {
		slog.Error("cron task failed", "job", job.Name, "err", err)
		job.LastResult = "task failed: " + err.Error()
		return
	}
	job.LastResult = result.Summary
	slog.Info("cron job done", "job", job.Name, "summary", result.Summary)
}

// sense runs all configured SenseTools and returns their combined output.
func (s *Scheduler) sense(ctx context.Context, job *Job) string {
	if len(job.SenseTools) == 0 {
		return "(no sense tools configured)"
	}
	var sb strings.Builder
	for _, toolName := range job.SenseTools {
		result, err := s.registry.Execute(ctx, toolName, nil)
		if err != nil {
			sb.WriteString(fmt.Sprintf("[%s error: %s]\n", toolName, err.Error()))
			continue
		}
		sb.WriteString(fmt.Sprintf("[%s]:\n%s\n\n", toolName, result))
	}
	return sb.String()
}

// extractJSON attempts to find and return the first JSON object in text.
func extractJSON(text string) string {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end < start {
		return "{}"
	}
	return text[start : end+1]
}

// matchCron is a simplified cron matcher supporting the 5-field format.
// For production use, replace with a proper cron library.
func matchCron(expr string, t time.Time) bool {
	if expr == "" || expr == "* * * * *" {
		return true
	}
	// Minimal implementation: only exact matches and wildcards.
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false
	}
	checks := []struct {
		field string
		val   int
	}{
		{fields[0], t.Minute()},
		{fields[1], t.Hour()},
		{fields[2], t.Day()},
		{fields[3], int(t.Month())},
		{fields[4], int(t.Weekday())},
	}
	for _, c := range checks {
		if c.field == "*" {
			continue
		}
		var v int
		if _, err := fmt.Sscanf(c.field, "%d", &v); err != nil {
			return false
		}
		if v != c.val {
			return false
		}
	}
	return true
}
