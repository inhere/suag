package agent_test

import (
	"testing"

	"github.com/inhere/axon/internal/agent"
	"github.com/inhere/axon/internal/agentcorestorage"
	"github.com/inhere/axon/internal/llm"
)

func TestNewRunner_DefaultsToAgentcore(t *testing.T) {
	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
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
