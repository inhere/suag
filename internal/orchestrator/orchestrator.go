// Package orchestrator ties together agents, teams, and routing logic.
package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/inhere/axon/internal/agent"
	"github.com/inhere/axon/internal/ops/hitl"
	"github.com/inhere/axon/internal/platform/llm"
	"github.com/inhere/axon/internal/platform/memory"
	"github.com/inhere/axon/internal/platform/tools"
	"github.com/inhere/axon/internal/platform/tracing"
	"github.com/inhere/axon/internal/team"
	"github.com/inhere/axon/pkg/agentcore/storage/sqlite"
)

// Config holds all dependencies for the Orchestrator.
type Config struct {
	LLM           llm.Provider
	Registry      *tools.Registry
	Tracer        tracing.Tracer
	HITL          hitl.Manager
	Team          *team.Manager
	Workspace     string
	MaxSteps      int
	FailThreshold int
	// Deprecated: keep only as a legacy config shim. Runtime selection should
	// use SingleAgentRuntime explicitly.
	UseAgentcore       bool
	SingleAgentRuntime string
	AgentcoreRunRoot   string
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

	switch o.singleAgentRuntime() {
	case "react":
		// React remains a legacy, explicit-only fallback while host v0 converges
		// all default single-agent execution on agentcore.
		runner, err := agent.NewRunner(a, agent.RunnerFactoryConfig{
			RuntimeKind: agent.RuntimeReact,
			Tracer:      o.cfg.Tracer,
			HITL:        o.cfg.HITL,
			MaxSteps:    o.cfg.MaxSteps,
		})
		if err != nil {
			return nil, err
		}
		return runner.Run(ctx, task)
	case "agentcore":
		storage, err := o.newAgentcoreStorage()
		if err != nil {
			return nil, fmt.Errorf("create agentcore storage: %w", err)
		}
		defer func() {
			_ = storage.Close()
		}()

		runner, err := agent.NewRunner(a, agent.RunnerFactoryConfig{
			RuntimeKind:   agent.RuntimeAgentcore,
			Storage:       storage,
			Tracer:        o.cfg.Tracer,
			HITL:          o.cfg.HITL,
			FailThreshold: o.cfg.FailThreshold,
			MaxSteps:      o.cfg.MaxSteps,
		})
		if err != nil {
			return nil, err
		}
		return runner.Run(ctx, task)
	default:
		return nil, fmt.Errorf("unsupported single agent runtime %q", o.cfg.SingleAgentRuntime)
	}
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

func (o *Orchestrator) newAgentcoreStorage() (*sqlite.SQLiteStorage, error) {
	root := o.cfg.AgentcoreRunRoot
	if root == "" {
		root = filepath.Join(o.cfg.Workspace, ".agentcore")
	}
	return sqlite.NewStorage(filepath.Join(root, "runs.db"))
}

func (o *Orchestrator) singleAgentRuntime() string {
	runtime := strings.ToLower(strings.TrimSpace(o.cfg.SingleAgentRuntime))
	if runtime != "" {
		return runtime
	}
	// The orchestrator now treats agentcore as the only default path. The old
	// UseAgentcore hint is ignored so React cannot become implicit again.
	return "agentcore"
}
