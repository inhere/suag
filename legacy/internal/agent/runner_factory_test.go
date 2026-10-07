package agent_test

import (
	"context"
	"testing"

	"github.com/inhere/suag/internal/agent"
	"github.com/inhere/suag/internal/platform/llm"
	"github.com/inhere/suag/pkg/agentcore/storage/sqlite"
)

func TestNewRunner_DefaultsToAgentcore(t *testing.T) {
	store, err := sqlite.NewStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	a := newTestAgent(llm.NewMockProvider("mock").QueueText("hello"), newTestRegistry())
	runner, err := agent.NewRunner(a, agent.RunnerFactoryConfig{
		Storage:  store,
		MaxSteps: 5,
	})
	if err != nil {
		t.Fatalf("NewRunner() error: %v", err)
	}

	if _, ok := runner.(*agent.AgentcoreRunner); !ok {
		t.Fatalf("runner type = %T, want *agent.AgentcoreRunner", runner)
	}
}

func TestNewRunner_CanSelectReact(t *testing.T) {
	a := newTestAgent(llm.NewMockProvider("mock").QueueText("hello"), newTestRegistry())
	runner, err := agent.NewRunner(a, agent.RunnerFactoryConfig{
		RuntimeKind: agent.RuntimeReact,
		MaxSteps:    5,
	})
	if err != nil {
		t.Fatalf("NewRunner() error: %v", err)
	}

	if _, ok := runner.(*agent.ReactRunner); !ok {
		t.Fatalf("runner type = %T, want *agent.ReactRunner", runner)
	}
}

func TestNewRunner_TrimmedReactSelectionRemainsExplicit(t *testing.T) {
	a := newTestAgent(llm.NewMockProvider("mock").QueueText("hello"), newTestRegistry())
	runner, err := agent.NewRunner(a, agent.RunnerFactoryConfig{
		RuntimeKind: agent.RuntimeKind(" React "),
		MaxSteps:    5,
	})
	if err != nil {
		t.Fatalf("NewRunner() error: %v", err)
	}

	if _, ok := runner.(*agent.ReactRunner); !ok {
		t.Fatalf("runner type = %T, want *agent.ReactRunner", runner)
	}
}

func TestNewRunner_ZeroMaxStepsDefaultsConsistently(t *testing.T) {
	store, err := sqlite.NewStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	reactAgent := newTestAgent(llm.NewMockProvider("react").QueueText("hello"), newTestRegistry())
	coreAgent := newTestAgent(llm.NewMockProvider("core").QueueText("hello"), newTestRegistry())

	reactRunner, err := agent.NewRunner(reactAgent, agent.RunnerFactoryConfig{
		RuntimeKind: agent.RuntimeReact,
		MaxSteps:    0,
	})
	if err != nil {
		t.Fatalf("react NewRunner() error: %v", err)
	}
	coreRunner, err := agent.NewRunner(coreAgent, agent.RunnerFactoryConfig{
		RuntimeKind: agent.RuntimeAgentcore,
		Storage:     store,
		MaxSteps:    0,
	})
	if err != nil {
		t.Fatalf("agentcore NewRunner() error: %v", err)
	}

	if _, err := reactRunner.Run(context.Background(), newTask("hello")); err != nil {
		t.Fatalf("react Run() error: %v", err)
	}
	if _, err := coreRunner.Run(context.Background(), newTask("hello")); err != nil {
		t.Fatalf("agentcore Run() error: %v", err)
	}
}

func TestNewRunner_PassesThroughPoliciesToAgentcoreRunner(t *testing.T) {
	store, err := sqlite.NewStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	a := newTestAgent(llm.NewMockProvider("mock").QueueText("hello"), newTestRegistry())

	resumePolicy := testResumePolicy{}
	toolPolicy := testToolExecutionPolicy{}
	errorPolicy := testErrorPolicy{}

	runner, err := agent.NewRunner(a, agent.RunnerFactoryConfig{
		Storage:             store,
		ResumePolicy:        resumePolicy,
		ToolExecutionPolicy: toolPolicy,
		ErrorPolicy:         errorPolicy,
	})
	if err != nil {
		t.Fatalf("NewRunner() error: %v", err)
	}

	coreRunner, ok := runner.(*agent.AgentcoreRunner)
	if !ok {
		t.Fatalf("runner type = %T, want *agent.AgentcoreRunner", runner)
	}

	cfg := agent.BuildAgentcoreConfig(a, agent.AgentcoreRunnerConfig{
		Storage:             store,
		ResumePolicy:        resumePolicy,
		ToolExecutionPolicy: toolPolicy,
		ErrorPolicy:         errorPolicy,
	})
	_ = coreRunner

	if cfg.ResumePolicy != resumePolicy {
		t.Fatalf("resume policy not forwarded")
	}
	if cfg.ToolExecutionPolicy != toolPolicy {
		t.Fatalf("tool execution policy not forwarded")
	}
	if cfg.ErrorPolicy != errorPolicy {
		t.Fatalf("error policy not forwarded")
	}
}
