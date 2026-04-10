// Package cron implements intelligent scheduled tasks (Agentic Cron).
package cron

import "time"

// Job defines a single agentic cron job.
type Job struct {
	ID             string
	Name           string
	Schedule       string   // cron expression e.g. "0 * * * *"
	SenseTools     []string // tool names called in the sense phase
	DecisionPrompt string   // prompt template; {{data}} is replaced with sense results
	AgentSoulPath  string   // optional SOUL.md for the execution agent
	Enabled        bool
	LastRunAt      time.Time
	LastResult     string
}
