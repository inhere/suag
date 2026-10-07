package team_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/inhere/suag/internal/ops/hitl"
	"github.com/inhere/suag/internal/platform/llm"
	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/internal/platform/tracing"
	"github.com/inhere/suag/internal/team"
)

func TestRunTeam_BlankRuntimeDefaultsToAgentcore(t *testing.T) {
	soulPath := writeSoulFile(t, "runner")
	runRoot := filepath.Join(t.TempDir(), "agentcore-runs")

	mgr := team.NewManager(
		llm.NewMockProvider("mock").QueueText("team task done"),
		tools.NewRegistry(),
		tracing.NewInMemoryTracer(nil),
		hitl.NewInMemoryManager(nil),
		t.TempDir(),
		team.ManagerConfig{AgentcoreRunRoot: runRoot},
	)

	tm, err := mgr.CreateTeam("ship it", []string{soulPath})
	if err != nil {
		t.Fatalf("CreateTeam() error: %v", err)
	}

	var agentID string
	for id := range tm.Agents {
		agentID = id
	}
	task := mgr.AssignTask(tm, "do work", "finish the task", agentID, nil)

	events, err := mgr.RunTeam(context.Background(), tm)
	if err != nil {
		t.Fatalf("RunTeam() error: %v", err)
	}
	drainEvents(events)

	if task.Status != team.TaskDone {
		t.Fatalf("task status = %s, want done", task.Status)
	}
	if _, err := os.Stat(filepath.Join(runRoot, "runs.db")); err != nil {
		t.Fatalf("expected agentcore storage file, got %v", err)
	}
}

func TestRunTeam_CanFallbackToReactRuntime(t *testing.T) {
	soulPath := writeSoulFile(t, "runner")
	runRoot := filepath.Join(t.TempDir(), "agentcore-runs")

	mgr := team.NewManager(
		llm.NewMockProvider("mock").QueueText("team task done"),
		tools.NewRegistry(),
		tracing.NewInMemoryTracer(nil),
		hitl.NewInMemoryManager(nil),
		t.TempDir(),
		team.ManagerConfig{Runtime: "react", AgentcoreRunRoot: runRoot},
	)

	tm, err := mgr.CreateTeam("ship it", []string{soulPath})
	if err != nil {
		t.Fatalf("CreateTeam() error: %v", err)
	}

	var agentID string
	for id := range tm.Agents {
		agentID = id
	}
	task := mgr.AssignTask(tm, "do work", "finish the task", agentID, nil)

	events, err := mgr.RunTeam(context.Background(), tm)
	if err != nil {
		t.Fatalf("RunTeam() error: %v", err)
	}
	drainEvents(events)

	if task.Status != team.TaskDone {
		t.Fatalf("task status = %s, want done", task.Status)
	}
	if _, err := os.Stat(filepath.Join(runRoot, "runs.db")); !os.IsNotExist(err) {
		t.Fatalf("expected no agentcore storage file in react mode, got err=%v", err)
	}
}

func writeSoulFile(t *testing.T, name string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, name+".md")
	content := "---\nname: " + name + "\nrole: worker\npersona: You are a helpful worker.\n---\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	return path
}

func drainEvents(events <-chan team.TeamEvent) {
	for range events {
	}
	time.Sleep(20 * time.Millisecond)
}
