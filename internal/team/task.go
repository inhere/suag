// Package team manages multi-agent teams and task coordination.
package team

import (
	"fmt"
	"time"
)

// TaskStatus mirrors agent.TaskStatus for team-level task tracking.
type TaskStatus string

const (
	TaskPending TaskStatus = "pending"
	TaskRunning TaskStatus = "running"
	TaskBlocked TaskStatus = "blocked"
	TaskDone    TaskStatus = "done"
	TaskFailed  TaskStatus = "failed"
)

// TeamTask is a unit of work managed by the Team.
type TeamTask struct {
	ID          string
	Title       string
	Description string
	AssignedTo  string // Agent ID
	Status      TaskStatus
	Result      string
	DependsOn   []string  // IDs of tasks that must complete before this one
	TraceID     string
	SpanID      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// newTaskID generates a unique task ID.
func newTaskID() string {
	return fmt.Sprintf("task-%d", time.Now().UnixNano())
}

// IsReady returns true when all dependencies are satisfied.
func (t *TeamTask) IsReady(done map[string]bool) bool {
	for _, dep := range t.DependsOn {
		if !done[dep] {
			return false
		}
	}
	return true
}
