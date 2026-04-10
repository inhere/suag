// Package agent provides the core Agent types, interfaces, and state management.
package agent

import (
	"context"
	"time"

	"github.com/inhere/axon/internal/platform/llm"
	"github.com/inhere/axon/internal/platform/memory"
	"github.com/inhere/axon/internal/platform/tools"
	"github.com/inhere/axon/internal/platform/tracing"
)

// AgentState represents the lifecycle state of an agent.
type AgentState string

const (
	AgentPending   AgentState = "pending"
	AgentRunning   AgentState = "running"
	AgentSuspended AgentState = "suspended" // HITL suspended
	AgentCompleted AgentState = "completed"
	AgentFailed    AgentState = "failed"
)

// Agent is the core execution unit. It carries its own memory, tools, LLM, and tracing context.
type Agent struct {
	ID        string
	Name      string
	Soul      *Soul
	State     AgentState
	Memory    memory.Store
	Tools     *tools.Registry
	LLM       llm.Provider
	ParentID  string // parent agent ID for subagents
	TraceID   string
	SpanID    string
	FailCount int
	CreatedAt time.Time
}

// Runner defines the interface for executing an agent on a task.
type Runner interface {
	Run(ctx context.Context, task *Task) (*TaskResult, error)
	Suspend(reason string) error
	Resume(instruction string) error
	GetState() AgentState
}

// Task is a unit of work assigned to an agent.
type Task struct {
	ID          string
	Title       string
	Description string
	AssignedTo  string // Agent ID
	Status      TaskStatus
	Result      string
	ParentID    string // parent task ID
	TraceID     string
	SpanID      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TaskStatus represents the lifecycle state of a task.
type TaskStatus string

const (
	TaskPending TaskStatus = "pending"
	TaskRunning TaskStatus = "running"
	TaskBlocked TaskStatus = "blocked" // HITL waiting
	TaskDone    TaskStatus = "done"
	TaskFailed  TaskStatus = "failed"
)

// TaskResult is the structured output from a completed task.
type TaskResult struct {
	TaskID  string
	Summary string
	Output  string
	Error   string
	Spans   []*tracing.Span
}

// Config holds configuration for creating a new agent.
type Config struct {
	Name      string
	SoulPath  string // path to SOUL.md
	LLM       llm.Provider
	Memory    memory.Store
	Tools     *tools.Registry
	MaxSteps  int
	Tracer    tracing.Tracer
	ParentID  string
	TraceID   string
	Workspace string // working directory for file/shell tools
}

// New creates a new Agent from the given config.
func New(cfg Config) (*Agent, error) {
	soul := &Soul{Name: cfg.Name}
	if cfg.SoulPath != "" {
		var err error
		soul, err = ParseSoul(cfg.SoulPath)
		if err != nil {
			return nil, err
		}
	}

	return &Agent{
		ID:        newID(),
		Name:      cfg.Name,
		Soul:      soul,
		State:     AgentPending,
		Memory:    cfg.Memory,
		Tools:     cfg.Tools,
		LLM:       cfg.LLM,
		ParentID:  cfg.ParentID,
		TraceID:   cfg.TraceID,
		CreatedAt: time.Now(),
	}, nil
}

// newID generates a unique agent ID.
func newID() string {
	// Uses a simple timestamp-based ID; replace with uuid in production.
	return "agent-" + time.Now().Format("20060102150405.000000000")
}
