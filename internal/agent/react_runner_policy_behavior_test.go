package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/inhere/axon/internal/agent"
	"github.com/inhere/axon/internal/llm"
	"github.com/inhere/axon/internal/tracing"
)

func TestReactRunner_Resume_UsesConfiguredResumePolicyBehavior(t *testing.T) {
	mock := llm.NewMockProvider("manual")
	a := newTestAgent(mock, newTestRegistry())
	a.State = agent.AgentRunning
	runner := agent.NewReactRunnerWithConfig(a, agent.ReactRunnerConfig{
		MaxSteps:     5,
		Tracer:       tracing.NewInMemoryTracer(nil),
		ResumePolicy: suppressResumeInstructionPolicy{},
	})

	if err := runner.Suspend("pause"); err != nil {
		t.Fatalf("Suspend() error: %v", err)
	}
	if err := runner.Resume("do not append this instruction"); err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	if msgs := a.Memory.GetMessages(); len(msgs) != 0 {
		t.Fatalf("memory messages = %#v, expected resume policy to suppress instruction injection", msgs)
	}
}

func TestReactRunner_Run_UsesConfiguredToolPolicyBehavior(t *testing.T) {
	mock := llm.NewMockProvider("mock").
		QueueToolCall("fail_tool", nil)
	a := newTestAgent(mock, newTestRegistry(&failTool{}))
	runner := agent.NewReactRunnerWithConfig(a, agent.ReactRunnerConfig{
		MaxSteps:            3,
		Tracer:              tracing.NewInMemoryTracer(nil),
		ToolExecutionPolicy: failOnToolErrorPolicy{},
	})

	result, err := runner.Run(context.Background(), newTask("tool policy failure"))
	if result != nil {
		t.Fatalf("expected tool policy failure to stop run, got %#v", result)
	}
	if err == nil || err.Error() != "tool policy escalated failure" {
		t.Fatalf("expected escalated tool policy error, got %v", err)
	}
	if runner.GetState() != agent.AgentFailed {
		t.Fatalf("state = %s, want failed", runner.GetState())
	}
}

func TestReactRunner_Run_UsesConfiguredErrorPolicyBehavior(t *testing.T) {
	mock := llm.NewMockProvider("mock").QueueError(errors.New("provider boom"))
	a := newTestAgent(mock, newTestRegistry())
	runner := agent.NewReactRunnerWithConfig(a, agent.ReactRunnerConfig{
		MaxSteps:    3,
		Tracer:      tracing.NewInMemoryTracer(nil),
		ErrorPolicy: yieldOnRecoverableErrorPolicy{},
	})

	result, err := runner.Run(context.Background(), newTask("recover provider failure"))
	if err != nil {
		t.Fatalf("expected error policy to convert failure into yield, got %v", err)
	}
	if result == nil || result.Summary != "policy_recoverable_error" || result.Output != "" {
		t.Fatalf("expected yielded task result from error policy, got %#v", result)
	}
	if runner.GetState() != agent.AgentSuspended {
		t.Fatalf("state = %s, want suspended", runner.GetState())
	}
}

func TestNewRunner_PassesThroughPoliciesToReactRunner(t *testing.T) {
	a := newTestAgent(llm.NewMockProvider("mock").QueueText("hello"), newTestRegistry())

	resumePolicy := suppressResumeInstructionPolicy{}
	toolPolicy := failOnToolErrorPolicy{}
	errorPolicy := yieldOnRecoverableErrorPolicy{}

	runner, err := agent.NewRunner(a, agent.RunnerFactoryConfig{
		RuntimeKind:         agent.RuntimeReact,
		MaxSteps:            5,
		ResumePolicy:        resumePolicy,
		ToolExecutionPolicy: toolPolicy,
		ErrorPolicy:         errorPolicy,
	})
	if err != nil {
		t.Fatalf("NewRunner() error: %v", err)
	}

	if _, ok := runner.(*agent.ReactRunner); !ok {
		t.Fatalf("runner type = %T, want *agent.ReactRunner", runner)
	}
}
