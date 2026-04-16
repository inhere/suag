package team

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/inhere/axon/internal/acstore"
	"github.com/inhere/axon/internal/agent"
	"github.com/inhere/axon/internal/ops/hitl"
	"github.com/inhere/axon/internal/platform/llm"
	"github.com/inhere/axon/internal/platform/memory"
	"github.com/inhere/axon/internal/platform/tools"
	"github.com/inhere/axon/internal/platform/tracing"
)

// TeamState represents the lifecycle of a team.
type TeamState string

const (
	TeamActive    TeamState = "active"
	TeamCompleted TeamState = "completed"
	TeamFailed    TeamState = "failed"
)

// Team is a group of agents working toward a common goal.
type Team struct {
	ID        string
	Name      string
	Goal      string
	Agents    map[string]*agent.Agent
	Tasks     []*TeamTask
	TraceID   string
	State     TeamState
	CreatedAt time.Time
}

// TeamEvent is emitted during team execution for SSE streaming.
type TeamEvent struct {
	Type    string // "task_started" | "task_done" | "task_failed" | "team_done"
	TeamID  string
	TaskID  string
	AgentID string
	Message string
}

// AgentFactory creates agents for the team.
type AgentFactory func(cfg agent.Config) (*agent.Agent, error)

// ManagerConfig controls how team tasks instantiate single-agent runtimes.
type ManagerConfig struct {
	Runtime          string
	AgentcoreRunRoot string
	FailThreshold    int
}

// Manager creates and coordinates teams.
type Manager struct {
	mu        sync.Mutex
	teams     map[string]*Team
	llm       llm.Provider
	registry  *tools.Registry
	tracer    tracing.Tracer
	hitlMgr   hitl.Manager
	workspace string
	cfg       ManagerConfig
}

// NewManager creates a Manager.
func NewManager(
	provider llm.Provider,
	registry *tools.Registry,
	tracer tracing.Tracer,
	hitlMgr hitl.Manager,
	workspace string,
	cfg ...ManagerConfig,
) *Manager {
	var resolved ManagerConfig
	if len(cfg) > 0 {
		resolved = cfg[0]
	}
	return &Manager{
		teams:     make(map[string]*Team),
		llm:       provider,
		registry:  registry,
		tracer:    tracer,
		hitlMgr:   hitlMgr,
		workspace: workspace,
		cfg:       resolved,
	}
}

// CreateTeam builds a new team from soul file paths and a shared goal.
func (m *Manager) CreateTeam(goal string, soulPaths []string) (*Team, error) {
	team := &Team{
		ID:        fmt.Sprintf("team-%d", time.Now().UnixNano()),
		Name:      "team",
		Goal:      goal,
		Agents:    make(map[string]*agent.Agent),
		TraceID:   tracing.NewTraceID(),
		State:     TeamActive,
		CreatedAt: time.Now(),
	}

	for _, path := range soulPaths {
		mem := memory.NewInMemStore(memory.InMemConfig{})
		a, err := agent.New(agent.Config{
			SoulPath:  path,
			LLM:       m.llm,
			Memory:    mem,
			Tools:     m.registry,
			TraceID:   team.TraceID,
			Workspace: m.workspace,
		})
		if err != nil {
			return nil, fmt.Errorf("create agent from %q: %w", path, err)
		}
		team.Agents[a.ID] = a
	}

	m.mu.Lock()
	m.teams[team.ID] = team
	m.mu.Unlock()

	return team, nil
}

// AssignTask adds a task to the team and assigns it to an agent.
func (m *Manager) AssignTask(team *Team, title, description, agentID string, dependsOn []string) *TeamTask {
	task := &TeamTask{
		ID:          newTaskID(),
		Title:       title,
		Description: description,
		AssignedTo:  agentID,
		Status:      TaskPending,
		DependsOn:   dependsOn,
		TraceID:     team.TraceID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	m.mu.Lock()
	team.Tasks = append(team.Tasks, task)
	m.mu.Unlock()
	return task
}

// RunTeam executes all team tasks respecting dependencies, streaming events on the returned channel.
// Tasks with no unresolved dependencies run concurrently.
func (m *Manager) RunTeam(ctx context.Context, team *Team) (<-chan TeamEvent, error) {
	evCh := make(chan TeamEvent, 64)

	go func() {
		defer close(evCh)
		m.runDAG(ctx, team, evCh)
	}()

	return evCh, nil
}

// runDAG executes tasks in dependency order, running independent tasks concurrently.
func (m *Manager) runDAG(ctx context.Context, team *Team, evCh chan<- TeamEvent) {
	done := make(map[string]bool)
	results := make(map[string]string) // task ID → result summary
	var mu sync.Mutex

	remaining := make([]*TeamTask, len(team.Tasks))
	copy(remaining, team.Tasks)

	for len(remaining) > 0 {
		// Find all tasks that are now ready.
		var ready []*TeamTask
		var notReady []*TeamTask
		mu.Lock()
		for _, t := range remaining {
			if t.IsReady(done) {
				ready = append(ready, t)
			} else {
				notReady = append(notReady, t)
			}
		}
		mu.Unlock()

		if len(ready) == 0 {
			// Deadlock: remaining tasks all blocked.
			slog.Error("team DAG deadlock", "team", team.ID, "remaining", len(notReady))
			break
		}

		// Execute ready tasks concurrently.
		var wg sync.WaitGroup
		for _, task := range ready {
			wg.Add(1)
			task := task
			go func() {
				defer wg.Done()
				a, ok := team.Agents[task.AssignedTo]
				if !ok {
					task.Status = TaskFailed
					task.Result = "agent not found: " + task.AssignedTo
					mu.Lock()
					done[task.ID] = true
					mu.Unlock()
					evCh <- TeamEvent{Type: "task_failed", TeamID: team.ID, TaskID: task.ID, Message: task.Result}
					return
				}

				task.Status = TaskRunning
				evCh <- TeamEvent{Type: "task_started", TeamID: team.ID, TaskID: task.ID, AgentID: a.ID}

				agentTask := &agent.Task{
					ID:          task.ID,
					Title:       task.Title,
					Description: enrichWithResults(task.Description, results, &mu),
					TraceID:     task.TraceID,
					CreatedAt:   task.CreatedAt,
				}

				runner, cleanup, err := m.newRunner(a)
				if err != nil {
					task.Status = TaskFailed
					task.Result = err.Error()
					mu.Lock()
					done[task.ID] = true
					mu.Unlock()
					evCh <- TeamEvent{Type: "task_failed", TeamID: team.ID, TaskID: task.ID, Message: err.Error()}
					return
				}
				if cleanup != nil {
					defer cleanup()
				}
				result, err := runner.Run(ctx, agentTask)

				mu.Lock()
				if err != nil {
					task.Status = TaskFailed
					task.Result = err.Error()
					done[task.ID] = true
					mu.Unlock()
					evCh <- TeamEvent{Type: "task_failed", TeamID: team.ID, TaskID: task.ID, Message: err.Error()}
					return
				}
				task.Status = TaskDone
				task.Result = result.Output
				results[task.ID] = result.Summary
				done[task.ID] = true
				mu.Unlock()
				evCh <- TeamEvent{Type: "task_done", TeamID: team.ID, TaskID: task.ID, AgentID: a.ID, Message: result.Summary}
			}()
		}
		wg.Wait()
		remaining = notReady
	}

	team.State = TeamCompleted
	evCh <- TeamEvent{Type: "team_done", TeamID: team.ID}
}

// enrichWithResults appends results of dependency tasks to the description.
func enrichWithResults(description string, results map[string]string, mu *sync.Mutex) string {
	mu.Lock()
	defer mu.Unlock()
	if len(results) == 0 {
		return description
	}
	extra := "\n\n[Context from previous tasks]:\n"
	for id, summary := range results {
		extra += fmt.Sprintf("- %s: %s\n", id, summary)
	}
	return description + extra
}

// DisbandTeam removes a team from the registry.
func (m *Manager) DisbandTeam(teamID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.teams, teamID)
}

func (m *Manager) newRunner(a *agent.Agent) (agent.Runner, func(), error) {
	switch m.runtime() {
	case "react":
		// React remains an explicit fallback for legacy callers only.
		runner, err := agent.NewRunner(a, agent.RunnerFactoryConfig{
			RuntimeKind: agent.RuntimeReact,
			Tracer:      m.tracer,
			HITL:        m.hitlMgr,
		})
		return runner, nil, err
	case "agentcore":
		storage, err := m.newAgentcoreStorage()
		if err != nil {
			return nil, nil, err
		}
		cleanup := func() {
			_ = storage.Close()
		}
		runner, err := agent.NewRunner(a, agent.RunnerFactoryConfig{
			RuntimeKind:   agent.RuntimeAgentcore,
			Storage:       storage,
			Tracer:        m.tracer,
			HITL:          m.hitlMgr,
			FailThreshold: m.cfg.FailThreshold,
		})
		return runner, cleanup, err
	default:
		return nil, nil, fmt.Errorf("unsupported team runtime %q", m.cfg.Runtime)
	}
}

func (m *Manager) runtime() string {
	runtime := strings.ToLower(strings.TrimSpace(m.cfg.Runtime))
	if runtime != "" {
		return runtime
	}
	// Team execution defaults to agentcore unless a legacy fallback is
	// explicitly requested.
	return "agentcore"
}

func (m *Manager) newAgentcoreStorage() (*agentcorestorage.SQLiteStorage, error) {
	root := m.cfg.AgentcoreRunRoot
	if root == "" {
		root = filepath.Join(m.workspace, ".agentcore")
	}
	return agentcorestorage.NewSQLiteStorage(filepath.Join(root, "runs.db"))
}
