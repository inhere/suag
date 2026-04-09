package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/inhere/axon/internal/agent"
	"github.com/inhere/axon/internal/agentcorestorage"
	"github.com/inhere/axon/internal/llm"
	"github.com/inhere/axon/pkg/agentcore"
)

type suppressResumeInstructionPolicy struct{}

func (suppressResumeInstructionPolicy) AllowResume(context.Context, agentcore.RunInput, *agentcore.RunState) error {
	return nil
}

func (suppressResumeInstructionPolicy) ShouldAppendInstruction(agentcore.RunInput, *agentcore.RunState) bool {
	return false
}

func (suppressResumeInstructionPolicy) MergeMetadata(stateMetadata, inputMetadata map[string]any) map[string]any {
	if len(stateMetadata) == 0 && len(inputMetadata) == 0 {
		return nil
	}
	out := make(map[string]any, len(stateMetadata)+len(inputMetadata))
	for k, v := range stateMetadata {
		out[k] = v
	}
	for k, v := range inputMetadata {
		out[k] = v
	}
	return out
}

type failOnToolErrorPolicy struct{}

func (failOnToolErrorPolicy) ValidateToolCalls(context.Context, *agentcore.RunState, []agentcore.ToolCall) error {
	return nil
}

func (failOnToolErrorPolicy) HandleToolError(_ context.Context, _ *agentcore.RunState, _ agentcore.ToolCall, _ error) agentcore.ToolErrorDecision {
	return agentcore.ToolErrorDecision{
		Fail: errors.New("tool policy escalated failure"),
	}
}

type yieldOnRecoverableErrorPolicy struct{}

func (yieldOnRecoverableErrorPolicy) HandleRecoverable(context.Context, *agentcore.RunState, *agentcore.ErrorEvent) agentcore.ErrorDecision {
	return agentcore.ErrorDecision{
		Result: &agentcore.RunResult{
			Status: agentcore.RunYielded,
			Yield: &agentcore.Yield{
				Reason:   "policy_recoverable_error",
				ResumeOK: true,
			},
		},
	}
}

func TestAgentcoreRunner_Run_UsesConfiguredResumePolicyBehavior(t *testing.T) {
	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	hook := &yieldBeforeSecondModelAgentHook{}
	mock := llm.NewMockProvider("manual").
		QueueToolCall("echo", map[string]any{"msg": "persisted"})
	a := newTestAgent(mock, newTestRegistry(&coreEchoTool{}))
	runner := agent.NewAgentcoreRunner(a, agent.AgentcoreRunnerConfig{
		Storage:      store,
		MaxSteps:     5,
		ResumePolicy: suppressResumeInstructionPolicy{},
		Hooks:        []agentcore.Hook{hook},
	})

	firstResult, err := runner.Run(context.Background(), newTask("resume policy behavior"))
	if err != nil {
		t.Fatalf("first Run() error: %v", err)
	}
	if firstResult.Summary != "await_resume" {
		t.Fatalf("first result summary = %q, want yield reason", firstResult.Summary)
	}

	if err := runner.Resume("do not append this instruction"); err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	done := make(chan parityRunOutcome, 1)
	go func() {
		r, runErr := runner.Run(context.Background(), newTask("resume policy behavior"))
		done <- parityRunOutcome{result: r, err: runErr}
	}()

	req, reply, err := mock.WaitForCall(context.Background())
	if err != nil {
		t.Fatalf("WaitForCall error: %v", err)
	}
	for _, msg := range req.Messages {
		if msg.Role == "user" && msg.Content == "do not append this instruction" {
			t.Fatalf("request messages = %#v, expected resume policy to suppress appended user input", req.Messages)
		}
	}
	reply(llm.MockResponse{Content: "completed without appended instruction"})

	outcome := waitAgentcoreOutcome(t, done)
	if outcome.err != nil {
		t.Fatalf("second Run() error: %v", outcome.err)
	}
	if outcome.result.Output != "completed without appended instruction" {
		t.Fatalf("output = %q, want resumed completion", outcome.result.Output)
	}
}

func TestAgentcoreRunner_Run_UsesConfiguredToolPolicyBehavior(t *testing.T) {
	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	mock := llm.NewMockProvider("mock").
		QueueToolCall("fail_tool", nil)
	a := newTestAgent(mock, newTestRegistry(&failTool{}))
	runner := agent.NewAgentcoreRunner(a, agent.AgentcoreRunnerConfig{
		Storage:             store,
		MaxSteps:            3,
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
	if mock.CallCount() != 1 {
		t.Fatalf("LLM called %d times, want 1", mock.CallCount())
	}
}

func TestAgentcoreRunner_Run_UsesConfiguredErrorPolicyBehavior(t *testing.T) {
	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	mock := llm.NewMockProvider("mock").QueueError(errors.New("provider boom"))
	a := newTestAgent(mock, newTestRegistry())
	runner := agent.NewAgentcoreRunner(a, agent.AgentcoreRunnerConfig{
		Storage:     store,
		MaxSteps:    3,
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
