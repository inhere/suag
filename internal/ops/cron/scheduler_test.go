package cron

import (
	"context"
	"testing"

	"github.com/inhere/axon/internal/orchestrator"
	"github.com/inhere/axon/internal/platform/llm"
	"github.com/inhere/axon/internal/platform/tools"
)

func TestScheduler_RunJob_CanParseNoActionDecision(t *testing.T) {
	provider := llm.NewMockProvider("mock").QueueText(`{"action":false,"reason":"nothing to do"}`)
	registry := tools.NewRegistry()
	orch := orchestrator.New(orchestrator.Config{
		LLM:              provider,
		Registry:         registry,
		AgentcoreRunRoot: t.TempDir(),
	})
	scheduler := NewScheduler(orch, provider, registry)

	job := &Job{
		ID:      "job-1",
		Name:    "decision-only",
		Enabled: true,
	}

	scheduler.runJob(context.Background(), job)

	if job.LastResult != "status normal: nothing to do" {
		t.Fatalf("LastResult = %q, want status normal decision", job.LastResult)
	}
	if provider.CallCount() != 1 {
		t.Fatalf("provider call count = %d, want 1", provider.CallCount())
	}
	calls := provider.Calls()
	if len(calls) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(calls))
	}
	if len(calls[0].Messages) != 1 || calls[0].Messages[0].Role != "user" {
		t.Fatalf("decision request messages = %#v, want single user prompt", calls[0].Messages)
	}
}

func TestScheduler_RunJob_CanTriggerOrchestratorExecution(t *testing.T) {
	provider := llm.NewMockProvider("mock").
		QueueText(`{"action":true,"reason":"needs follow-up"}`).
		QueueText("cron execution completed")
	registry := tools.NewRegistry()
	orch := orchestrator.New(orchestrator.Config{
		LLM:              provider,
		Registry:         registry,
		AgentcoreRunRoot: t.TempDir(),
	})
	scheduler := NewScheduler(orch, provider, registry)

	job := &Job{
		ID:      "job-2",
		Name:    "action-job",
		Enabled: true,
	}

	scheduler.runJob(context.Background(), job)

	if job.LastResult != "cron execution completed" {
		t.Fatalf("LastResult = %q, want orchestrator summary", job.LastResult)
	}
	if provider.CallCount() != 2 {
		t.Fatalf("provider call count = %d, want 2", provider.CallCount())
	}
}
