// Package orchestrator ties together agents, teams, and routing logic.
package orchestrator

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/inhere/axon/internal/agent"
	"github.com/inhere/axon/internal/hitl"
	"github.com/inhere/axon/internal/llm"
	"github.com/inhere/axon/internal/memory"
	"github.com/inhere/axon/internal/team"
	"github.com/inhere/axon/internal/tools"
	"github.com/inhere/axon/internal/tracing"
)

// Config holds all dependencies for the Orchestrator.
type Config struct {
	LLM       llm.Provider
	Registry  *tools.Registry
	Tracer    tracing.Tracer
	HITL      hitl.Manager
	Team      *team.Manager
	Workspace string
	MaxSteps  int
}

// Orchestrator is the top-level coordinator. It receives tasks, decides whether
// to use a single agent or a team, and runs execution.
type Orchestrator struct {
	cfg Config
}

// New creates an Orchestrator from cfg.
func New(cfg Config) *Orchestrator {
	return &Orchestrator{cfg: cfg}
}

// RunTask processes a single user task with a standalone agent.
func (o *Orchestrator) RunTask(ctx context.Context, description string) (*agent.TaskResult, error) {
	traceID := tracing.NewTraceID()
	ctx = tracing.WithTrace(ctx, tracing.TraceContext{TraceID: traceID})

	slog.Info("orchestrator run task", "trace", traceID, "desc", description[:min(60, len(description))])

	mem := memory.NewInMemStore(memory.InMemConfig{})
	a, err := agent.New(agent.Config{
		Name:      "default",
		LLM:       o.cfg.LLM,
		Memory:    mem,
		Tools:     o.cfg.Registry,
		TraceID:   traceID,
		Workspace: o.cfg.Workspace,
	})
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}

	task := &agent.Task{
		ID:          fmt.Sprintf("task-%s", traceID[:8]),
		Title:       "user task",
		Description: description,
		TraceID:     traceID,
	}

	runner := agent.NewReactRunner(a, o.cfg.MaxSteps, o.cfg.Tracer, o.cfg.HITL)
	return runner.Run(ctx, task)
}

// RunTeamTask creates a team from soul paths and executes tasks as a DAG.
func (o *Orchestrator) RunTeamTask(ctx context.Context, goal string, soulPaths []string) (<-chan team.TeamEvent, error) {
	t, err := o.cfg.Team.CreateTeam(goal, soulPaths)
	if err != nil {
		return nil, fmt.Errorf("create team: %w", err)
	}
	return o.cfg.Team.RunTeam(ctx, t)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
