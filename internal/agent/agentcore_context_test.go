package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/inhere/axon/internal/agent"
	"github.com/inhere/axon/internal/agentcorestorage"
	"github.com/inhere/axon/internal/llm"
	"github.com/inhere/axon/internal/memory"
)

type contextAwareMemoryStore struct {
	messages []memory.Message
}

func (s *contextAwareMemoryStore) AddMessage(role, content string) {
	s.messages = append(s.messages, memory.Message{Role: role, Content: content})
}

func (s *contextAwareMemoryStore) GetMessages() []memory.Message {
	out := make([]memory.Message, len(s.messages))
	copy(out, s.messages)
	return out
}

func (s *contextAwareMemoryStore) GetContext(context.Context, int) ([]memory.Message, error) {
	out := []memory.Message{
		{Role: "system", Content: "[Memory summary]: context from internal memory"},
	}
	out = append(out, s.GetMessages()...)
	return out, nil
}

func (s *contextAwareMemoryStore) SaveFragment(_ context.Context, _, _ string) error { return nil }

func (s *contextAwareMemoryStore) SearchSimilar(context.Context, string, int) ([]memory.Fragment, error) {
	return nil, nil
}

func (s *contextAwareMemoryStore) Close() error { return nil }

func TestAgentcoreRunner_Run_UsesInternalMemoryContextByDefault(t *testing.T) {
	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	mock := llm.NewMockProvider("manual")
	mem := &contextAwareMemoryStore{}
	a, err := agent.New(agent.Config{
		Name:   "context-agent",
		LLM:    mock,
		Memory: mem,
		Tools:  newTestRegistry(),
	})
	if err != nil {
		t.Fatalf("agent.New() error: %v", err)
	}

	runner := agent.NewAgentcoreRunner(a, agent.AgentcoreRunnerConfig{
		Storage:  store,
		MaxSteps: 5,
	})

	ch := make(chan parityRunOutcome, 1)
	go func() {
		r, runErr := runner.Run(context.Background(), newTask("use memory context"))
		ch <- parityRunOutcome{result: r, err: runErr}
	}()

	req, reply, err := mock.WaitForCall(context.Background())
	if err != nil {
		t.Fatalf("WaitForCall error: %v", err)
	}
	if len(req.Messages) < 2 {
		t.Fatalf("messages = %#v, want memory summary plus user input", req.Messages)
	}
	if req.Messages[0].Role != "system" || req.Messages[0].Content != "[Memory summary]: context from internal memory" {
		t.Fatalf("first request message = %#v, want memory summary", req.Messages[0])
	}
	if req.Messages[1].Role != "user" || req.Messages[1].Content != "use memory context" {
		t.Fatalf("second request message = %#v, want user instruction from memory context", req.Messages[1])
	}
	reply(llm.MockResponse{Content: "done"})

	outcome := waitAgentcoreOutcome(t, ch)
	if outcome.err != nil {
		t.Fatalf("Run() error: %v", outcome.err)
	}
	if outcome.result.Output != "done" {
		t.Fatalf("output = %q, want final answer", outcome.result.Output)
	}
}

func waitAgentcoreOutcome(t *testing.T, ch <-chan parityRunOutcome) parityRunOutcome {
	t.Helper()

	select {
	case outcome := <-ch:
		return outcome
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for agentcore outcome")
		return parityRunOutcome{}
	}
}
